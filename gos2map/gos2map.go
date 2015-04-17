package gos2map

import (
	"bytes"
	"encoding/json"
	"fmt"
	"html/template"
	"net/http"

	"strconv"

	"appengine"
	"appengine/datastore"

	"github.com/davidreynolds/geojson"
	"github.com/davidreynolds/gos2/s2"
	"github.com/gorilla/mux"
)

const defaultFeatureCollection = `{
  "type": "FeatureCollection",
  "features": [],
}`

type GeoJSON struct {
	Name string
	JSON string `datastore:",noindex"`
}

var indexPage = template.Must(template.ParseFiles("templates/index.html"))

func indexHandler(w http.ResponseWriter, r *http.Request) {
	c := appengine.NewContext(r)
	name := RandomName()
	obj := GeoJSON{
		Name: name,
		JSON: defaultFeatureCollection,
	}
	key := datastore.NewKey(c, "GeoJSON", name, 0, nil)
	if _, err := datastore.Put(c, key, &obj); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	http.Redirect(w, r, fmt.Sprintf("/%s", name), http.StatusFound)
}

func mapHandler(w http.ResponseWriter, r *http.Request) {
	c := appengine.NewContext(r)
	vars := mux.Vars(r)
	key := datastore.NewKey(c, "GeoJSON", vars["name"], 0, nil)
	var obj GeoJSON
	if err := datastore.Get(c, key, &obj); err != nil {
		http.Error(w, "404 Not Found", http.StatusNotFound)
		return
	}
	if err := indexPage.Execute(w, obj); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
}

func updateEditor(w http.ResponseWriter, r *http.Request) {
	c := appengine.NewContext(r)
	vars := mux.Vars(r)
	key := datastore.NewKey(c, "GeoJSON", vars["name"], 0, nil)
	buf := new(bytes.Buffer)
	buf.ReadFrom(r.Body)
	obj := GeoJSON{
		Name: vars["name"],
		JSON: buf.String(),
	}
	if _, err := datastore.Put(c, key, &obj); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
}

type LatLng struct {
	Lat float64 `json:"lat"`
	Lng float64 `json:"lng"`
}

type CellIDJSON struct {
	Id       string    `json:"id"`
	IdSigned string    `json:"id_signed"`
	Token    string    `json:"token"`
	Pos      string    `json:"pos"`
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
		idJson.Id = strconv.FormatUint(uint64(cell.Id()), 10)
		idJson.IdSigned = strconv.FormatInt(int64(cell.Id()), 10)
		idJson.Token = cell.Id().ToToken()
		idJson.Pos = strconv.FormatUint(cell.Id().Pos(), 10)
		idJson.Face = cell.Id().Face()
		idJson.Level = cell.Id().Level()
		covering = append(covering, idJson)
	}
	enc := json.NewEncoder(w)
	if err := enc.Encode(covering); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
	}
}

func coverHandler(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Content-Encoding", "gzip")
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
		maxCells = 8
	}
	levelMod, err := strconv.Atoi(r.FormValue("level_mod"))
	if err != nil {
		levelMod = 1
	}

	var geojs geojson.GeoJSON
	err = geojson.Unmarshal([]byte(r.FormValue("geojson")), &geojs)
	if hasError(w, err) {
		return
	}
	coverer := s2.NewRegionCoverer()
	coverer.SetMinLevel(minLevel)
	coverer.SetMaxLevel(maxLevel)
	coverer.SetLevelMod(levelMod)
	coverer.SetMaxCells(maxCells)
	coverMap := make(map[s2.CellID]struct{})
	switch geojs := geojs.(type) {
	case geojson.FeatureCollection:
		for _, feature := range geojs.Features {
			geom := feature.Geometry
			poly, err := geometryToS2Polygon(geom)
			if hasError(w, err) {
				return
			}
			cover := coverer.Covering(poly)
			for _, c := range cover {
				coverMap[c] = struct{}{}
			}
		}
	}
	covering := make([]s2.CellID, 0, len(coverMap))
	for k, _ := range coverMap {
		covering = append(covering, k)
	}
	cellIdsToJSON(w, covering)
}

func hasError(w http.ResponseWriter, err error) bool {
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return true
	}
	return false
}

func union(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	decoder := json.NewDecoder(r.Body)
	var m map[string]interface{}
	err := decoder.Decode(&m)
	if hasError(w, err) {
		return
	}
	var js geojson.GeoJSON
	geojson.FromMap(m, &js)
	collection, err := Union(js)
	if hasError(w, err) {
		return
	}
	enc := json.NewEncoder(w)
	if err := enc.Encode(collection); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
	}
}

func intersection(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	decoder := json.NewDecoder(r.Body)
	var m map[string]interface{}
	err := decoder.Decode(&m)
	if hasError(w, err) {
		return
	}
	var js geojson.GeoJSON
	geojson.FromMap(m, &js)
	collection, err := Intersection(js)
	if hasError(w, err) {
		return
	}
	enc := json.NewEncoder(w)
	if err := enc.Encode(collection); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
	}
}

func difference(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	decoder := json.NewDecoder(r.Body)
	var m map[string]interface{}
	err := decoder.Decode(&m)
	if hasError(w, err) {
		return
	}
	var js geojson.GeoJSON
	geojson.FromMap(m, &js)
	collection, err := Difference(js)
	if hasError(w, err) {
		return
	}
	enc := json.NewEncoder(w)
	if err := enc.Encode(collection); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
	}
}

func symmetricDifference(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	decoder := json.NewDecoder(r.Body)
	var m map[string]interface{}
	err := decoder.Decode(&m)
	if hasError(w, err) {
		return
	}
	var js geojson.GeoJSON
	geojson.FromMap(m, &js)
	collection, err := SymmetricDifference(js)
	if hasError(w, err) {
		return
	}
	enc := json.NewEncoder(w)
	if err := enc.Encode(collection); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
	}
}

func init() {
	r := mux.NewRouter()
	r.HandleFunc("/", indexHandler)
	r.HandleFunc("/{name:[a-zA-Z]+}", mapHandler).Methods("GET")
	r.HandleFunc("/{name:[a-zA-Z]+}", updateEditor).Methods("POST")
	r.HandleFunc("/a/s2cover", coverHandler)
	r.HandleFunc("/a/union", union)
	r.HandleFunc("/a/intersection", intersection)
	r.HandleFunc("/a/difference", difference)
	r.HandleFunc("/a/symmetric_difference", symmetricDifference)
	http.Handle("/", r)
}
