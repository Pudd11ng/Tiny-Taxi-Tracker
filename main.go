package main

import (
	"encoding/json"
	"log"
	"net/http"
)

// LocationResponse represents the JSON payload returned by the /location endpoint.
type LocationResponse struct {
	Driver string  `json:"driver"`
	Lat    float64 `json:"lat"`
	Lng    float64 `json:"lng"`
}

func locationHandler(w http.ResponseWriter, r *http.Request) {
	resp := LocationResponse{
		Driver: "Ali",
		Lat:    1.29,
		Lng:    103.85,
	}

	w.Header().Set("Content-Type", "application/json")
	if err := json.NewEncoder(w).Encode(resp); err != nil {
		http.Error(w, "failed to encode response", http.StatusInternalServerError)
		return
	}
}

func main() {
	http.HandleFunc("/location", locationHandler)

	log.Println("🚕 Tiny Taxi Tracker running on :8080")
	if err := http.ListenAndServe(":8080", nil); err != nil {
		log.Fatalf("server failed to start: %v", err)
	}
}
