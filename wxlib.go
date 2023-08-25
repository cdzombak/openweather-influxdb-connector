package main

import "math"

// TempFToC converts the given Fahrenheit temperature to Celsius
func TempFToC(tempF float64) float64 {
	return (tempF - 32.0) / 1.8
}

// TempCToF converts the given Celsius temperature to Fahrenheit
func TempCToF(tempC float64) float64 {
	return tempC*1.8 + 32.0
}

// DewPoint calculates the dew point given the current temperature (in Fahrenheit)
// and relative humidity percentage (an integer 0-100, *not* a float 0.0-1.0).
func DewPoint(tempF float64, relH int) float64 {
	const (
		a = 17.625
		b = 243.04
	)
	t := TempFToC(tempF)
	alpha := math.Log(float64(relH)/100.0) + a*t/(b+t)
	dewPointC := (b * alpha) / (a - alpha)
	return TempCToF(dewPointC)
}

// WindChill calculates the wind chill for the given temperature (in Fahrenheit)
// and wind speed (in miles/hour). If wind speed is less than 3 mph, or temperature
// if over 50 degrees, the given temperature is returned - the forumla works
// below 50 degrees and above 3 mph.
//
// This is taken from ecobee_influx_connector as this is supposed to be an identical
// weather stand-in:
// https://github.com/cdzombak/ecobee_influx_connector/blob/cf49c9c64291ac1858a79b879dc515385cf61c67/main.go#L48-L57
func WindChill(tempF, windSpeedMph float64) float64 {
	if tempF > 50.0 || windSpeedMph < 3.0 {
		return tempF
	}
	return 35.74 + (0.6215 * tempF) - (35.75 * math.Pow(windSpeedMph, 0.16)) + (0.4275 * tempF * math.Pow(windSpeedMph, 0.16))
}

// IndoorHumidityRecommendation returns the maximum recommended indoor relative
// humidity percentage for the given outdoor temperature (in degrees F).
//
// This is taken from ecobee_influx_connector as this is supposed to be an identical
// weather stand-in:
// https://github.com/cdzombak/ecobee_influx_connector/blob/cf49c9c64291ac1858a79b879dc515385cf61c67/main.go#L59-L84
func IndoorHumidityRecommendation(outdoorTempF float64) int {
	if outdoorTempF >= 50 {
		return 50
	}
	if outdoorTempF >= 40 {
		return 45
	}
	if outdoorTempF >= 30 {
		return 40
	}
	if outdoorTempF >= 20 {
		return 35
	}
	if outdoorTempF >= 10 {
		return 30
	}
	if outdoorTempF >= 0 {
		return 25
	}
	if outdoorTempF >= -10 {
		return 20
	}
	return 15
}
