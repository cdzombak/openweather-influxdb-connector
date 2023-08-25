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
	printData := flag.Bool("printData", false, "Print weather.pollution data to stdout.")
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
		authString = fmt.Sprintf("%s", config.InfluxToken)
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
	influxWriteApi := influxClient.WriteAPIBlocking(config.InfluxOrg, config.InfluxBucket)

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
	outdoorTemp := wx.Main.Temp
	feelsLikeTemp := wx.Main.FeelsLike
	// nb. OpenWeatherMap reports pressure in hPa regardless of unit setting; hPa == millibar
	pressureMillibar := wx.Main.Pressure
	outdoorHumidity := wx.Main.Humidity // int, in %
	dewpoint := DewPoint(outdoorTemp, outdoorHumidity)
	windspeedMph := wx.Wind.Speed
	windBearing := wx.Wind.Deg
	visibilityMeters := wx.Visibility
	visibilityMiles := float64(visibilityMeters) / 1609.34
	windChill := WindChill(outdoorTemp, windspeedMph)
	cloudsPercent := wx.Clouds.All
	// TODO(cdzombak): record weather condition codes from wx.Weather
	//                 see https://openweathermap.org/current

	if *printData {
		fmt.Printf("Weather at %s:\n", weatherTime)
		fmt.Printf("\ttemperature: %.1f degF\n\tpressure: %.0f mb\n\thumidity: %d%%\n\tdew point: %.1f degF\n\twind: %.0f at %.1f mph\n\twind chill: %.1f degF\n\tvisibility: %.1f miles\n\tcloud cover: %d%%",
			outdoorTemp, pressureMillibar, outdoorHumidity, dewpoint, windBearing, windspeedMph, windChill, visibilityMiles, cloudsPercent)
	}

	if config.WriteEcobeeWeatherMeasurement {
		if err := retry.Do(func() error {
			ctx, cancel := context.WithTimeout(context.Background(), influxTimeout)
			defer cancel()
			err := influxWriteApi.WritePoint(ctx,
				influxdb2.NewPoint(
					ecobeeWeatherMeasurementName,
					map[string]string{
						thermostatNameTag: config.EcobeeThermostatName,
						sourceTag:         source,
					},
					map[string]interface{}{
						"outdoor_temp":                    outdoorTemp,
						"outdoor_humidity":                outdoorHumidity,
						"barometric_pressure_mb":          pressureMillibar,
						"barometric_pressure_inHg":        float64(pressureMillibar) / 33.864,
						"dew_point":                       dewpoint,
						"wind_speed":                      windspeedMph,
						"wind_bearing":                    windBearing,
						"visibility_mi":                   visibilityMiles,
						"recommended_max_indoor_humidity": IndoorHumidityRecommendation(outdoorTemp),
						"wind_chill_f":                    windChill,
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
		err := influxWriteApi.WritePoint(ctx,
			influxdb2.NewPoint(
				config.WeatherMeasurementName,
				map[string]string{
					sourceTag: source,
					latTag:    strconv.FormatFloat(config.Latitude, 'f', 3, 64),
					lonTag:    strconv.FormatFloat(config.Longitude, 'f', 3, 64),
				},
				map[string]interface{}{
					"temp_f":                          outdoorTemp,
					"temp_c":                          TempFToC(outdoorTemp),
					"rel_humidity":                    outdoorHumidity,
					"feels_like_f":                    feelsLikeTemp,
					"feels_like_c":                    TempFToC(feelsLikeTemp),
					"barometric_pressure_mb":          pressureMillibar,
					"barometric_pressure_inHg":        float64(pressureMillibar) / 33.864,
					"dew_point_f":                     dewpoint,
					"dew_point_c":                     TempFToC(dewpoint),
					"wind_speed_mph":                  windspeedMph,
					"wind_bearing":                    windBearing,
					"visibility_mi":                   visibilityMiles,
					"recommended_max_indoor_humidity": IndoorHumidityRecommendation(outdoorTemp),
					"wind_chill_f":                    windChill,
					"wind_chill_c":                    TempFToC(windChill),
					"cloud_cover":                     cloudsPercent,
				},
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

	//goland:noinspection GoStructInitializationWithoutFieldNames
	aqiUsParticulates, err := aqi.Calculate(
		aqi.PM25{polData.Components.Pm25},
		aqi.PM10{polData.Components.Pm10},
	)
	if err != nil {
		log.Fatalf("Failed to calculate US AQI for particulates: %s", err)
	}
	//goland:noinspection GoStructInitializationWithoutFieldNames
	aqiUs, err := aqi.Calculate(
		aqi.PM25{polData.Components.Pm25},
		aqi.PM10{polData.Components.Pm10},
		aqi.CO{polData.Components.Co},
		aqi.NO2{polData.Components.No2},
		aqi.SO2{polData.Components.So2},
	)
	if err != nil {
		log.Fatalf("Failed to calculate overall US AQI: %s", err)
	}

	// TODO(cdzombak): log pollution the same way we do weather above

	if err := retry.Do(func() error {
		ctx, cancel := context.WithTimeout(context.Background(), influxTimeout)
		defer cancel()
		err := influxWriteApi.WritePoint(ctx,
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
