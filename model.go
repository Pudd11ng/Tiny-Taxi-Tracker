package main

import "time"

// LocationUpdate is the payload received from drivers (POST /location).
type LocationUpdate struct {
	DriverID string  `json:"driver_id"`
	Lat      float64 `json:"lat"`
	Lng      float64 `json:"lng"`
}

// LocationResponse is what we return to clients.
type LocationResponse struct {
	DriverID  string    `json:"driver_id"`
	Lat       float64   `json:"lat"`
	Lng       float64   `json:"lng"`
	UpdatedAt time.Time `json:"updated_at"`
}
