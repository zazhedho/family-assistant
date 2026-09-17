package dto

type Location struct {
	Code string `json:"code"`
	Name string `json:"name"`
}

type SyncLocationRequest struct {
	Year         string `json:"year" binding:"omitempty"`
	Level        string `json:"level" binding:"omitempty,oneof=all province city district village"`
	ProvinceCode string `json:"province_code" binding:"omitempty"`
	CityCode     string `json:"city_code" binding:"omitempty"`
	DistrictCode string `json:"district_code" binding:"omitempty"`
}

type LocationSyncJob struct {
	ID            string `json:"id"`
	Status        string `json:"status"`
	Level         string `json:"level"`
	Year          string `json:"year"`
	ProvinceCode  string `json:"province_code,omitempty"`
	CityCode      string `json:"city_code,omitempty"`
	DistrictCode  string `json:"district_code,omitempty"`
	RequestedBy   string `json:"requested_by_user_id"`
	Message       string `json:"message,omitempty"`
	ErrorMessage  string `json:"error_message,omitempty"`
	ProvinceCount int    `json:"province_count"`
	CityCount     int    `json:"city_count"`
	DistrictCount int    `json:"district_count"`
	VillageCount  int    `json:"village_count"`
	StartedAt     string `json:"started_at,omitempty"`
	FinishedAt    string `json:"finished_at,omitempty"`
	CreatedAt     string `json:"created_at,omitempty"`
	UpdatedAt     string `json:"updated_at,omitempty"`
}
