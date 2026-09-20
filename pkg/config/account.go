package config

import (
	"time"

	"family-assistant/utils"
)

func LoadMinimumIndependentAccountAge() int {
	age := utils.GetEnv("MIN_INDEPENDENT_ACCOUNT_AGE", 18)
	if age < 1 {
		return 18
	}
	return age
}

func AgeOn(birthDate, today time.Time) int {
	age := today.Year() - birthDate.Year()
	birthdayDay := birthDate.Day()
	if birthDate.Month() == time.February && birthDate.Day() == 29 && !isLeapYear(today.Year()) {
		birthdayDay = 28
	}
	if today.Month() < birthDate.Month() || (today.Month() == birthDate.Month() && today.Day() < birthdayDay) {
		age--
	}
	return age
}

func isLeapYear(year int) bool {
	return year%400 == 0 || (year%4 == 0 && year%100 != 0)
}
