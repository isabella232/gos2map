package gos2map

import (
	"encoding/json"
	"html/template"
	"net/http"
	"strconv"
	"strings"

	"github.com/davidreynolds/gos2/s2"
	"github.com/gorilla/mux"
)

func IndexHandler(w http.ResponseWriter, r *http.Request) {
	t, err := template.ParseFiles("templates/index.html")
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	t.Execute(w, nil)
}

type LatLng struct {
	Lat float64 `json:"lat"`
	Lng float64 `json:"lng"`
}

type CellIDJSON struct {
	Id       uint64    `json:"id"`
	IdSigned int64     `json:"id_signed"`
	Token    string    `json:"token"`
	Pos      uint64    `json:"pos"`
	Face     int       `json:"face"`
	Level    int       `json:"level"`
	LL       LatLng    `json:"ll"`
	Shape    [4]LatLng `json:"shape"`
}

func cellIdsToJSON(w http.ResponseWriter, ids []s2.CellID) {
	covering := []CellIDJSON{}
	for _, id := range ids {
		idJson := CellIDJSON{}
		cell := s2.CellFromCellID(id)
		center := s2.LatLngFromPoint(cell.Center())
		for i := 0; i < 4; i++ {
			ll := s2.LatLngFromPoint(cell.Vertex(i))
			idJson.Shape[i].Lat = ll.Lat.Degrees()
			idJson.Shape[i].Lng = ll.Lng.Degrees()
		}
		idJson.LL.Lat = center.Lat.Degrees()
		idJson.LL.Lng = center.Lng.Degrees()
		idJson.Id = uint64(cell.Id())
		idJson.IdSigned = int64(cell.Id())
		idJson.Token = cell.Id().ToToken()
		idJson.Pos = cell.Id().Pos()
		idJson.Face = cell.Id().Face()
		idJson.Level = cell.Id().Level()
		covering = append(covering, idJson)
	}
	js, err := json.Marshal(&covering)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	w.Write(js)
}

func S2CoverHandler(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	points := strings.Split(r.FormValue("points"), ",")
	minLevel, err := strconv.Atoi(r.FormValue("min_level"))
	if err != nil {
		minLevel = 1
	}
	maxLevel, err := strconv.Atoi(r.FormValue("max_level"))
	if err != nil {
		maxLevel = s2.MaxCellLevel
	}
	maxCells, err := strconv.Atoi(r.FormValue("max_cells"))
	if err != nil {
		maxCells = 200
	}
	levelMod, err := strconv.Atoi(r.FormValue("level_mod"))
	if err != nil {
		levelMod = 1
	}
	builder := s2.NewPolygonBuilder(s2.DIRECTED_XOR())
	pvec := []s2.Point{}
	for i := 0; i < len(points); i += 2 {
		lat, _ := strconv.ParseFloat(points[i], 64)
		lng, _ := strconv.ParseFloat(points[i+1], 64)
		pvec = append(pvec, s2.PointFromLatLng(s2.LatLngFromDegrees(lat, lng)))
	}

	var covering []s2.CellID
	if len(pvec) == 1 {
		for i := minLevel; i <= maxLevel; i += levelMod {
			covering = append(covering, s2.CellIDFromPoint(pvec[0]).Parent(i))
		}
	} else {
		for i := 0; i < len(pvec); i++ {
			builder.AddEdge(pvec[i], pvec[(i+1)%len(pvec)])
		}

		var poly s2.Polygon
		builder.AssemblePolygon(&poly, nil)

		coverer := s2.NewRegionCoverer()
		coverer.SetMinLevel(minLevel)
		coverer.SetMaxLevel(maxLevel)
		coverer.SetLevelMod(levelMod)
		coverer.SetMaxCells(maxCells)
		covering = coverer.Covering(poly)
	}
	cellIdsToJSON(w, covering)
}

func init() {
	r := mux.NewRouter()
	r.HandleFunc("/", IndexHandler)
	r.HandleFunc("/api/s2cover", S2CoverHandler)
	http.Handle("/", r)
}
