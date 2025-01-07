package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"log"
	"os"
	"strconv"
	"time"

	"github.com/avast/retry-go"
	owm "github.com/briandowns/openweathermap"
	"github.com/cdzombak/libwx"
	influxdb2 "github.com/influxdata/influxdb-client-go/v2"
	"github.com/mrflynn/go-aqi"
)

var version = "<dev>"

const (
	influxTimeout    = 3 * time.Second
	influxAttempts   = 3
	influxRetryDelay = 1 * time.Second

	source                       = "openweathermap"
	sourceTag                    = "data_source"
	thermostatNameTag            = "thermostat_name"
	latTag                       = "latitude"
	lonTag                       = "longitude"
	ecobeeWeatherMeasurementName = "ecobee_weather"
)

// Config describes the configuration for the openweather-influxdb-connector program.
type Config struct {
	APIKey                        string  `json:"api_key"`
	Latitude                      float64 `json:"lat"`
	Longitude                     float64 `json:"lon"`
	InfluxServer                  string  `json:"influx_server"`
	InfluxOrg                     string  `json:"influx_org,omitempty"`
	InfluxUser                    string  `json:"influx_user,omitempty"`
	InfluxPass                    string  `json:"influx_password,omitempty"`
	InfluxToken                   string  `json:"influx_token,omitempty"`
	InfluxBucket                  string  `json:"influx_bucket"`
	InfluxHealthCheckDisabled     bool    `json:"influx_health_check_disabled"`
	WeatherMeasurementName        string  `json:"wx_measurement_name"`
	WriteEcobeeWeatherMeasurement bool    `json:"write_ecobee_weather_measurement"`
	EcobeeThermostatName          string  `json:"ecobee_thermostat_name"`
	PollutionMeasurementName      string  `json:"pollution_measurement_name"`
}

func main() {
	configFile := flag.String("config", "./config.json", "Configuration JSON file.")
	printData := flag.Bool("printData", false, "Print weather/pollution data to stdout.")
	printVersion := flag.Bool("version", false, "Print version and exit.")
	flag.Parse()

	if *printVersion {
		fmt.Println(version)
		os.Exit(0)
	}

	if *configFile == "" {
		fmt.Println("-config is required.")
		os.Exit(1)
	}

	config := Config{}
	cfgBytes, err := os.ReadFile(*configFile)
	if err != nil {
		log.Fatalf("Unable to read config file '%s': %s", *configFile, err)
	}
	if err = json.Unmarshal(cfgBytes, &config); err != nil {
		log.Fatalf("Unable to parse config file '%s': %s", *configFile, err)
	}
	if config.APIKey == "" {
		log.Fatal("api_key must be set in the config file.")
	}
	if config.WeatherMeasurementName == "" {
		log.Fatal("wx_measurement_name must be set in the config file.")
	}
	if config.WriteEcobeeWeatherMeasurement && config.EcobeeThermostatName == "" {
		log.Fatal("ecobee_thermostat_name must be set in the config file if write_ecobee_wx_measurement is set.")
	}

	authString := ""
	if config.InfluxUser != "" || config.InfluxPass != "" {
		authString = fmt.Sprintf("%s:%s", config.InfluxUser, config.InfluxPass)
	} else if config.InfluxToken != "" {
		authString = config.InfluxToken
	}
	influxClient := influxdb2.NewClient(config.InfluxServer, authString)
	if !config.InfluxHealthCheckDisabled {
		ctx, cancel := context.WithTimeout(context.Background(), influxTimeout)
		defer cancel()
		health, err := influxClient.Health(ctx)
		if err != nil {
			log.Fatalf("Failed to check InfluxDB health: %v", err)
		}
		if health.Status != "pass" {
			log.Fatalf("InfluxDB did not pass health check: status %s; message '%s'", health.Status, *health.Message)
		}
	}
	influxWriteAPI := influxClient.WriteAPIBlocking(config.InfluxOrg, config.InfluxBucket)

	configCoords := owm.Coordinates{
		Longitude: config.Longitude,
		Latitude:  config.Latitude,
	}

	wx, err := owm.NewCurrent("F", "EN", config.APIKey)
	if err != nil {
		log.Fatalf("Failed to create OpenWeatherMap current weather client: %s", err)
	}

	if err := wx.CurrentByCoordinates(&configCoords); err != nil {
		log.Fatalf("Failed to get weather from OpenWeatherMap: %s", err)
	}

	// see response docs at: https://openweathermap.org/current#parameter
	weatherTime := time.Unix(int64(wx.Dt), 0)
	outdoorTemp := libwx.TempF(wx.Main.Temp)
	feelsLikeTemp := libwx.TempF(wx.Main.FeelsLike)
	// nb. OpenWeatherMap reports pressure in hPa regardless of unit setting; hPa == millibar
	pressureMillibar := libwx.PressureMb(wx.Main.Pressure)
	outdoorHumidity := libwx.ClampedRelHumidity(wx.Main.Humidity) // int, in %
	dewpoint := libwx.DewPointF(outdoorTemp, outdoorHumidity)
	windSpeedMph := libwx.SpeedMph(wx.Wind.Speed)
	windBearing := wx.Wind.Deg
	visibilityMeters := libwx.Meter(wx.Visibility)
	visibilityMiles := visibilityMeters.Miles()
	cloudsPercent := wx.Clouds.All
	// TODO(cdzombak): record weather condition codes from wx.Weather
	//                 see https://openweathermap.org/weather-conditions#Weather-Condition-Codes-2

	if *printData {
		fmt.Printf("Conditions at %s:\n", weatherTime)
		fmt.Printf("\ttemperature: %.1f degF\n\tpressure: %.0f mb\n\thumidity: %d%%\n\tdew point: %.1f degF\n\twind: %.0f at %.1f mph\n\tvisibility: %.1f miles\n\tcloud cover: %d%%",
			outdoorTemp, pressureMillibar, outdoorHumidity, dewpoint, windBearing, windSpeedMph, visibilityMiles, cloudsPercent)
	}

	heatIdxF, heatIdxFErr := libwx.HeatIndexFWithValidation(outdoorTemp, outdoorHumidity)
	heatIdxC, heatIdxCErr := libwx.HeatIndexCWithValidation(outdoorTemp.C(), outdoorHumidity)
	windChillF, windChillFErr := libwx.WindChillFWithValidation(outdoorTemp, windSpeedMph)
	windChillC, windChillCErr := libwx.WindChillCWithValidation(outdoorTemp.C(), windSpeedMph)
	wetBulbTempF, wetBulbTempFErr := libwx.WetBulbF(outdoorTemp, outdoorHumidity)
	wetBulbTempC, wetBulbTempCErr := libwx.WetBulbC(outdoorTemp.C(), outdoorHumidity)

	if config.WriteEcobeeWeatherMeasurement {
		if err := retry.Do(func() error {
			ctx, cancel := context.WithTimeout(context.Background(), influxTimeout)
			defer cancel()
			err := influxWriteAPI.WritePoint(ctx,
				influxdb2.NewPoint(
					ecobeeWeatherMeasurementName,
					map[string]string{
						thermostatNameTag: config.EcobeeThermostatName,
						sourceTag:         source,
					},
					map[string]interface{}{
						"outdoor_temp":                    outdoorTemp.Unwrap(),
						"outdoor_humidity":                outdoorHumidity.Unwrap(),
						"barometric_pressure_mb":          pressureMillibar.Unwrap(),
						"barometric_pressure_inHg":        pressureMillibar.InHg().Unwrap(),
						"dew_point":                       dewpoint.Unwrap(),
						"wind_speed":                      windSpeedMph.Unwrap(),
						"wind_bearing":                    windBearing,
						"visibility_mi":                   visibilityMiles.Unwrap(),
						"recommended_max_indoor_humidity": libwx.IndoorHumidityRecommendationF(outdoorTemp).Unwrap(),
						"wind_chill_f":                    windChillF.Unwrap(),
					},
					weatherTime,
				))
			if err != nil {
				return err
			}
			return nil
		}, retry.Attempts(influxAttempts), retry.Delay(influxRetryDelay)); err != nil {
			log.Printf("Failed to write %s to influx: %s", ecobeeWeatherMeasurementName, err)
		}
	}

	if err := retry.Do(func() error {
		ctx, cancel := context.WithTimeout(context.Background(), influxTimeout)
		defer cancel()
		fields := map[string]interface{}{
			"temp_f":                            outdoorTemp.Unwrap(),
			"temp_c":                            outdoorTemp.C().Unwrap(),
			"rel_humidity":                      outdoorHumidity.Unwrap(),
			"feels_like_f":                      feelsLikeTemp.Unwrap(),
			"feels_like_c":                      feelsLikeTemp.C().Unwrap(),
			"barometric_pressure_mb":            pressureMillibar.Unwrap(),
			"barometric_pressure_inHg":          pressureMillibar.InHg().Unwrap(),
			"dew_point_f":                       dewpoint.Unwrap(),
			"dew_point_c":                       dewpoint.C().Unwrap(),
			"wind_speed_mph":                    windSpeedMph.Unwrap(),
			"wind_speed_kt":                     windSpeedMph.Knots().Unwrap(),
			"wind_bearing":                      windBearing,
			"visibility_mi":                     visibilityMiles.Unwrap(),
			"recommended_max_indoor_humidity_f": libwx.IndoorHumidityRecommendationF(outdoorTemp).Unwrap(),
			"recommended_max_indoor_humidity_c": libwx.IndoorHumidityRecommendationC(outdoorTemp.C()).Unwrap(),
			"cloud_cover":                       cloudsPercent,
		}

		if heatIdxFErr == nil {
			fields["heat_index_f"] = heatIdxF.Unwrap()
		}
		if heatIdxCErr == nil {
			fields["heat_index_c"] = heatIdxC.Unwrap()
		}
		if windChillFErr == nil {
			fields["wind_chill_f"] = windChillF.Unwrap()
		}
		if windChillCErr == nil {
			fields["wind_chill_c"] = windChillC.Unwrap()
		}
		if wetBulbTempFErr == nil {
			fields["wet_bulb_f"] = wetBulbTempF.Unwrap()
		}
		if wetBulbTempCErr == nil {
			fields["wet_bulb_c"] = wetBulbTempC.Unwrap()
		}

		err := influxWriteAPI.WritePoint(ctx,
			influxdb2.NewPoint(
				config.WeatherMeasurementName,
				map[string]string{
					sourceTag: source,
					latTag:    strconv.FormatFloat(config.Latitude, 'f', 3, 64),
					lonTag:    strconv.FormatFloat(config.Longitude, 'f', 3, 64),
				},
				fields,
				weatherTime,
			))
		if err != nil {
			return err
		}
		return nil
	}, retry.Attempts(influxAttempts), retry.Delay(influxRetryDelay)); err != nil {
		log.Printf("Failed to write %s to influx: %s", config.WeatherMeasurementName, err)
	}

	// Pollution: https://openweathermap.org/api/air-pollution
	polResp, err := owm.NewPollution(config.APIKey)
	if err != nil {
		log.Fatalf("Failed to create OpenWeatherMap pollution client: %s", err)
	}
	if err := polResp.PollutionByParams(&owm.PollutionParameters{
		Location: configCoords,
		Datetime: "current", // unused internally by the library but it appears in the example code, so ...
	}); err != nil {
		log.Fatalf("Failed to get pollution from OpenWeatherMap: %s", err)
	}
	if len(polResp.List) == 0 {
		log.Fatal("OpenWeatherMap didn't return any pollution information")
	}
	polData := polResp.List[0]

	aqiUsParticulates, err := aqi.Calculate(
		aqi.PM25{Concentration: polData.Components.Pm25},
		aqi.PM10{Concentration: polData.Components.Pm10},
	)
	if err != nil {
		log.Fatalf("Failed to calculate US AQI for particulates: %s", err)
	}
	aqiUs, err := aqi.Calculate(
		aqi.PM25{Concentration: polData.Components.Pm25},
		aqi.PM10{Concentration: polData.Components.Pm10},
		aqi.CO{Concentration: polData.Components.Co},
		aqi.NO2{Concentration: polData.Components.No2},
		aqi.SO2{Concentration: polData.Components.So2},
	)
	if err != nil {
		log.Fatalf("Failed to calculate overall US AQI: %s", err)
	}

	if *printData {
		fmt.Printf("Pollution at %s:\n", weatherTime)
		fmt.Printf("\tAQI (US EPA): %.1f\n\tAQI (US EPA, particulates): %.1f\n\tCO: %.2f\n\tNO: %.2f\n\tNO2: %.2f\n\tO3: %.2f\n\tSO2: %.2f\n\tPM2.5: %.2f\n\tPM10: %.2f\n\tNH3: %.2f\n",
			aqiUs.AQI, aqiUsParticulates.AQI, polData.Components.Co, polData.Components.No, polData.Components.No2, polData.Components.O3, polData.Components.So2, polData.Components.Pm25, polData.Components.Pm10, polData.Components.Nh3)
	}

	if err := retry.Do(func() error {
		ctx, cancel := context.WithTimeout(context.Background(), influxTimeout)
		defer cancel()
		err := influxWriteAPI.WritePoint(ctx,
			influxdb2.NewPoint(
				config.PollutionMeasurementName,
				map[string]string{
					sourceTag: source,
					latTag:    strconv.FormatFloat(config.Latitude, 'f', 3, 64),
					lonTag:    strconv.FormatFloat(config.Longitude, 'f', 3, 64),
				},
				map[string]interface{}{
					"aqi_1_5":        polData.Main.Aqi,
					"aqi_us_pm":      aqiUsParticulates.AQI,
					"aqi_us_pm_name": aqiUsParticulates.Index.Name,
					"aqi_us":         aqiUs.AQI,
					"aqi_us_name":    aqiUs.Index.Name,
					"co":             polData.Components.Co,
					"no":             polData.Components.No,
					"no2":            polData.Components.No2,
					"o3":             polData.Components.O3,
					"so2":            polData.Components.So2,
					"pm25":           polData.Components.Pm25,
					"pm10":           polData.Components.Pm10,
					"nh3":            polData.Components.Nh3,
				},
				time.Unix(int64(polData.Dt), 0),
			))
		if err != nil {
			return err
		}
		return nil
	}, retry.Attempts(influxAttempts), retry.Delay(influxRetryDelay)); err != nil {
		log.Printf("Failed to write %s to influx: %s", config.PollutionMeasurementName, err)
	}
}
