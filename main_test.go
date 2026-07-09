package main

import (
	"math"
	"testing"

	"github.com/cdzombak/libwx"
)

func TestStationPressure(t *testing.T) {
	const seaLevel = libwx.PressureMb(1013.25)

	// at sea level, station pressure is sea level pressure
	if got := stationPressure(seaLevel, 0, libwx.TempC(20)); math.Abs(got.Unwrap()-seaLevel.Unwrap()) > 0.001 {
		t.Errorf("at 0m: expected %v, got %v", seaLevel, got)
	}

	// Ann Arbor, ~256m: roughly 30mb below sea level pressure
	got := stationPressure(seaLevel, 256, libwx.TempC(20))
	if math.Abs(got.Unwrap()-983.5) > 0.5 {
		t.Errorf("at 256m: expected ~983.5 mb, got %v", got)
	}

	// station pressure must decrease as elevation increases
	prev := stationPressure(seaLevel, 0, libwx.TempC(20))
	for elevation := 100.0; elevation <= 4000; elevation += 100 {
		cur := stationPressure(seaLevel, elevation, libwx.TempC(20))
		if cur.Unwrap() >= prev.Unwrap() {
			t.Fatalf("station pressure must decrease with elevation, but at %vm got %v (was %v)", elevation, cur, prev)
		}
		prev = cur
	}
}

func TestWetBulb(t *testing.T) {
	annArborStation := libwx.PressureMb(983.5)

	t.Run("uses Sadeghi when a station pressure is available", func(t *testing.T) {
		result, method, err := wetBulb(libwx.TempC(20), libwx.RelHumidity(50), &annArborStation)
		if err != nil {
			t.Fatalf("unexpected error: %s", err)
		}
		if method != wetBulbMethodSadeghi {
			t.Errorf("expected method %s, got %s", wetBulbMethodSadeghi, method)
		}
		// slightly below the sea level value of 13.84 degC
		if math.Abs(result.Unwrap()-13.76) > 0.1 {
			t.Errorf("expected ~13.76 degC, got %v", result)
		}
	})

	t.Run("falls back to Stull when no station pressure is available", func(t *testing.T) {
		result, method, err := wetBulb(libwx.TempC(20), libwx.RelHumidity(50), nil)
		if err != nil {
			t.Fatalf("unexpected error: %s", err)
		}
		if method != wetBulbMethodStull {
			t.Errorf("expected method %s, got %s", wetBulbMethodStull, method)
		}
		expected, _ := libwx.WetBulbC(libwx.TempC(20), libwx.RelHumidity(50))
		if result != expected {
			t.Errorf("expected the Stull value %v, got %v", expected, result)
		}
	})

	t.Run("falls back to Stull below Sadeghi's -17C lower bound", func(t *testing.T) {
		// -20C at 80% RH is outside Sadeghi's range but inside Stull's; a value
		// must still be produced, or cold winter nights become gaps in Influx
		result, method, err := wetBulb(libwx.TempC(-20), libwx.RelHumidity(80), &annArborStation)
		if err != nil {
			t.Fatalf("unexpected error: %s", err)
		}
		if method != wetBulbMethodStull {
			t.Errorf("expected method %s, got %s", wetBulbMethodStull, method)
		}
		if result.Unwrap() > -20 {
			t.Errorf("wet bulb must not exceed dry bulb, got %v", result)
		}
	})

	t.Run("reports an error when neither formula supports the input", func(t *testing.T) {
		// 100% RH is outside both formulas' supported range
		_, method, err := wetBulb(libwx.TempC(20), libwx.RelHumidity(100), &annArborStation)
		if err == nil {
			t.Error("expected an error for out-of-range relative humidity")
		}
		if method != wetBulbMethodStull {
			t.Errorf("expected method %s, got %s", wetBulbMethodStull, method)
		}
	})
}
