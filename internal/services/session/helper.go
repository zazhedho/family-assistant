package servicesession

import "strings"

func extractDeviceInfo(userAgent string) string {
	if strings.Contains(userAgent, "Mobile") || strings.Contains(userAgent, "Android") || strings.Contains(userAgent, "iPhone") {
		if strings.Contains(userAgent, "Android") {
			return "Android Mobile"
		} else if strings.Contains(userAgent, "iPhone") {
			return "iOS Mobile"
		}
		return "Mobile Device"
	} else if strings.Contains(userAgent, "iPad") || strings.Contains(userAgent, "Tablet") {
		return "Tablet"
	} else if strings.Contains(userAgent, "Windows") {
		return "Windows PC"
	} else if strings.Contains(userAgent, "Macintosh") || strings.Contains(userAgent, "Mac OS") {
		return "Mac"
	} else if strings.Contains(userAgent, "Linux") {
		return "Linux"
	}

	return "Unknown Device"
}
