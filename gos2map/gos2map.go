package gos2map

import (
	"bytes"
	"encoding/json"
	"fmt"
	"html/template"
	"log"
	"net/http"

	"strconv"
	"strings"

	"appengine"
	"appengine/datastore"

	"github.com/davidreynolds/gos2/s2"
	"github.com/gorilla/mux"
	"github.com/kpawlik/geojson"
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
	var points []string
	pointstr := r.FormValue("points")
	if len(pointstr) > 0 {
		points = strings.Split(r.FormValue("points"), ",")
	}
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
	builder := s2.NewPolygonBuilder(s2.DIRECTED_XOR())
	pvec := []s2.Point{}
	for i := 0; i < len(points); i += 2 {
		lat, _ := strconv.ParseFloat(points[i], 64)
		lng, _ := strconv.ParseFloat(points[i+1], 64)
		pvec = append(pvec, s2.PointFromLatLng(s2.LatLngFromDegrees(lat, lng)))
	}

	var covering []s2.CellID
	coverer := s2.NewRegionCoverer()
	coverer.SetMinLevel(minLevel)
	coverer.SetMaxLevel(maxLevel)
	coverer.SetLevelMod(levelMod)
	coverer.SetMaxCells(maxCells)

	var feature geojson.Feature
	err = json.Unmarshal([]byte(r.FormValue("geojson")), &feature)
	if hasError(w, err) {
		return
	}

	geom, err := feature.GetGeometry()
	if hasError(w, err) {
		return
	}
	switch gg := geom.(type) {
	case *geojson.Polygon:
		for i := 0; i < len(gg.Coordinates); i++ {
			coords := gg.Coordinates[i]
			pb := s2.NewPolygonBuilder(s2.DIRECTED_XOR())
			vec := make([]s2.Point, 0, len(coords)/2)
			for j := 0; j < len(coords); j += 2 {
				lat := float64(coords[j][1])
				lng := float64(coords[j][0])
				vec = append(vec, s2.PointFromLatLng(s2.LatLngFromDegrees(lat, lng)))
			}
			for j := 0; j < len(vec); j++ {
				pb.AddEdge(vec[j], vec[(j+1)%len(vec)])
			}
			var poly s2.Polygon
			pb.AssemblePolygon(&poly, nil)
			builder.AddPolygon(&poly)
		}
		var poly s2.Polygon
		builder.AssemblePolygon(&poly, nil)
		covering = coverer.Covering(&poly)
	default:
		log.Printf("Invalid geom type: %v", gg)
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
	var fc FeatureCollection
	err := decoder.Decode(&fc)
	if hasError(w, err) {
		return
	}
	features, err := fc.Union()
	if hasError(w, err) {
		return
	}
	collection := geojson.NewFeatureCollection(features)
	enc := json.NewEncoder(w)
	if err := enc.Encode(collection); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
	}
}

func intersection(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	decoder := json.NewDecoder(r.Body)
	var fc FeatureCollection
	err := decoder.Decode(&fc)
	if hasError(w, err) {
		return
	}
	features, err := fc.Intersection()
	if hasError(w, err) {
		return
	}
	collection := geojson.NewFeatureCollection(features)
	enc := json.NewEncoder(w)
	if err := enc.Encode(collection); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
	}
}

func difference(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	decoder := json.NewDecoder(r.Body)
	var fc FeatureCollection
	err := decoder.Decode(&fc)
	if hasError(w, err) {
		return
	}
	features, err := fc.Difference()
	if hasError(w, err) {
		return
	}
	collection := geojson.NewFeatureCollection(features)
	enc := json.NewEncoder(w)
	if err := enc.Encode(collection); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
	}
}

func symmetricDifference(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	decoder := json.NewDecoder(r.Body)
	var fc FeatureCollection
	err := decoder.Decode(&fc)
	if hasError(w, err) {
		return
	}
	features, err := fc.SymmetricDifference()
	if hasError(w, err) {
		return
	}
	collection := geojson.NewFeatureCollection(features)
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
