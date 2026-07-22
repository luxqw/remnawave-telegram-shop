package config

import "testing"

func TestApplyRuntimeSettings_OverridesPrice(t *testing.T) {
	conf.price1 = 100
	defer ApplyRuntimeSettings(nil)

	if got := Price1(); got != 100 {
		t.Fatalf("want default 100, got %d", got)
	}

	ApplyRuntimeSettings(map[string]string{"PRICE_1": "150"})
	if got := Price1(); got != 150 {
		t.Fatalf("want overridden 150, got %d", got)
	}
	if got := Price(1); got != 150 {
		t.Fatalf("Price(1) should reflect the override too, got %d", got)
	}
}

func TestApplyRuntimeSettings_InvalidValueFallsBackToDefault(t *testing.T) {
	conf.price3 = 200
	defer ApplyRuntimeSettings(nil)

	ApplyRuntimeSettings(map[string]string{"PRICE_3": "not-a-number"})
	if got := Price3(); got != 200 {
		t.Fatalf("want fallback to default 200 on invalid override, got %d", got)
	}
}

func TestApplyRuntimeSettings_UnrelatedKeyLeavesOtherPricesUntouched(t *testing.T) {
	conf.price1 = 100
	conf.price6 = 500
	defer ApplyRuntimeSettings(nil)

	ApplyRuntimeSettings(map[string]string{"PRICE_1": "999"})
	if got := Price6(); got != 500 {
		t.Fatalf("overriding PRICE_1 should not affect PRICE_6, got %d", got)
	}
}

func TestDeviceSlotDailyPriceRUB_UsesOverride(t *testing.T) {
	conf.deviceSlotPriceRUB = 90
	conf.daysInMonth = 30
	defer ApplyRuntimeSettings(nil)

	if got := DeviceSlotDailyPriceRUB(); got != 3 {
		t.Fatalf("want 90/30=3 before override, got %v", got)
	}

	ApplyRuntimeSettings(map[string]string{"DEVICE_SLOT_PRICE_RUB": "150"})
	if got := DeviceSlotDailyPriceRUB(); got != 5 {
		t.Fatalf("want 150/30=5 after override, got %v", got)
	}
}
