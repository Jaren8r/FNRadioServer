package main

import (
	"context"
	"database/sql"
	"fmt"
	"os"

	"github.com/jackc/pgx/v4/pgxpool"
)

type User struct {
	ID     string
	Secret string
}

type Binding struct {
	ID          string `json:"id"`
	StationUser string `json:"station_user"`
	StationID   string `json:"station_id"`
}

type Station struct {
	UserID string         `json:"user_id,omitempty"`
	ID     string         `json:"id"`
	Type   string         `json:"type"`
	Source sql.NullString `json:"-"`
}

func (server *FNRadioServer) setupDB() {
	var err error

	server.DB, err = pgxpool.Connect(context.Background(), os.Getenv("DATABASE_URL"))
	if err != nil {
		panic(err)
	}

	file, err := os.ReadFile("init.sql")
	if err != nil {
		return
	}

	_, err = server.DB.Exec(context.Background(), string(file))
	if err != nil {
		panic(err)
	}
}

func (server *FNRadioServer) getUserStations(user string) ([]Station, error) {
	var stations []Station

	rows, err := server.DB.Query(context.TODO(), "SELECT id, type, source FROM stations WHERE user_id = $1", user)
	if err != nil {
		return nil, err
	}

	for rows.Next() {
		var station Station

		err = rows.Scan(&station.ID, &station.Type, &station.Source)
		if err != nil {
			return nil, err
		}

		stations = append(stations, station)
	}

	if stations == nil {
		return make([]Station, 0), nil // Return an empty array instead of nil if there are no stations
	}

	return stations, nil
}

func (server *FNRadioServer) getUserBindings(user string) ([]Binding, error) {
	var bindings []Binding

	rows, err := server.DB.Query(context.TODO(), "SELECT id, station_user, station_id FROM bindings WHERE user_id = $1", user)
	if err != nil {
		return nil, err
	}

	for rows.Next() {
		var binding Binding

		err = rows.Scan(&binding.ID, &binding.StationUser, &binding.StationID)
		if err != nil {
			return nil, err
		}

		bindings = append(bindings, binding)
	}

	if bindings == nil {
		return make([]Binding, 0), nil // Return an empty array instead of nil if there are no bindings
	}

	return bindings, nil
}

func (server *FNRadioServer) getUserStation(user string, stationID string) *Station {
	var station Station

	err := server.DB.QueryRow(context.TODO(), "SELECT type, source FROM stations WHERE user_id = $1 AND id = $2", user, stationID).Scan(&station.Type, &station.Source)
	if err != nil {
		return nil
	}

	return &station
}

func (server *FNRadioServer) getUserBinding(user string, id string) *Binding {
	var binding Binding

	err := server.DB.QueryRow(context.TODO(), "SELECT station_user, station_id FROM bindings WHERE user_id = $1 AND id = $2", user, id).Scan(&binding.StationUser, &binding.StationID)
	if err != nil {
		fmt.Println(err)
		return nil
	}

	return &binding
}
