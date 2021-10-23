package tilepack

import (
	"database/sql"
	"log"
	"fmt"
	_ "github.com/mattn/go-sqlite3" // Register sqlite3 database driver
)

type TileData struct {
	Tile *Tile
	Data *[]byte
}

type ZoomRange struct {
	Min int
	Max int
}

type TileExtent struct {
	MinX float64
	MinY float64	
	MaxX float64
	MaxY float64	
}	
type MbtilesReader interface {
	Close() error
	GetTile(tile *Tile) (*TileData, error)
	VisitAllTiles(visitor func(*Tile, []byte)) error
	GetZoomRange() (*ZoomRange, error)
	GetTileExtent() (*TileExtent, error)
}

type tileDataFromDatabase struct {
	Data *[]byte
}

func NewMbtilesReader(dsn string) (MbtilesReader, error) {
	db, err := sql.Open("sqlite3", dsn)
	if err != nil {
		return nil, err
	}
	return NewMbtilesReaderWithDatabase(db)
}

func NewMbtilesReaderWithDatabase(db *sql.DB) (MbtilesReader, error) {
	return &mbtilesReader{db: db}, nil
}

type mbtilesReader struct {
	MbtilesReader
	db *sql.DB
}

// Close gracefully tears down the mbtiles connection.
func (o *mbtilesReader) Close() error {
	var err error

	if o.db != nil {
		if err2 := o.db.Close(); err2 != nil {
			err = err2
		}
	}

	return err
}

// GetTile returns data for the given tile.
func (o *mbtilesReader) GetTile(tile *Tile) (*TileData, error) {
	var data []byte

	result := o.db.QueryRow("SELECT tile_data FROM tiles WHERE zoom_level=? AND tile_column=? AND tile_row=? LIMIT 1", tile.Z, tile.X, tile.Y)
	err := result.Scan(&data)

	if err != nil {
		if err == sql.ErrNoRows {
			blankTile := &TileData{Tile: tile, Data: nil}
			return blankTile, nil
		}
		return nil, err
	}

	tileData := &TileData{
		Tile: tile,
		Data: &data,
	}

	return tileData, nil
}

// VisitAllTiles runs the given function on all tiles in this mbtiles archive.
func (o *mbtilesReader) VisitAllTiles(visitor func(*Tile, []byte)) error {
	rows, err := o.db.Query("SELECT zoom_level, tile_column, tile_row, tile_data FROM tiles")
	if err != nil {
		return err
	}

	var z, x, y uint
	for rows.Next() {
		data := []byte{}
		err := rows.Scan(&z, &x, &y, &data)
		if err != nil {
			log.Printf("Couldn't scan row: %+v", err)
		}

		t := &Tile{Z: z, X: x, Y: y}
		visitor(t, data)
	}
	return nil
}

func (o *mbtilesReader) GetTileExtent() (*TileExtent, error ){

	return nil, fmt.Errorf("Not implemented")
}

func (o *mbtilesReader) GetZoomRange() (*ZoomRange, error ){

	min, err := o.getMinZoom()

	if err != nil {
		return nil, err
	}
	
	max, err := o.getMaxZoom()
	
	if err != nil {
		return nil, err
	}

	r := &ZoomRange{
		Min: min,
		Max: max,
	}

	return r, nil
}

func (o *mbtilesReader) getMinZoom() (int, error ){

	q := "SELECT MIN(zoom_level) FROM tiles"

	row := o.db.QueryRow(q)

	var z int
	err := row.Scan(&z)

	if err != nil {
		return 0, err
	}

	return z, nil
}

func (o *mbtilesReader) getMaxZoom() (int, error ){

	q := "SELECT MAX(zoom_level) FROM tiles"

	row := o.db.QueryRow(q)

	var z int
	err := row.Scan(&z)

	if err != nil {
		return 0, err
	}

	return z, nil	
}
