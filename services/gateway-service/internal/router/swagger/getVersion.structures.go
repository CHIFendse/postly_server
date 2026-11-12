package swagger

type VersionResponse struct {
	Success bool   `json:"success" example:"true"`
	Version string `json:"version" example:"1.4.2"`
}