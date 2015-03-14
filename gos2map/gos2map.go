package gos2map

import (
	"encoding/json"
	"errors"
	"html/template"
	"log"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/davidreynolds/gos2/s2"
	"github.com/gorilla/mux"
	"github.com/kpawlik/geojson"
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

func S2CoverHandler(w http.ResponseWriter, r *http.Request) {
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

	if len(pvec) == 0 {
		// Try geojson.
		var feature geojson.Feature
		err := json.Unmarshal([]byte(r.FormValue("geojson")), &feature)
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
		}
	} else if len(pvec) == 1 {
		for i := minLevel; i <= maxLevel; i += levelMod {
			covering = append(covering, s2.CellIDFromPoint(pvec[0]).Parent(i))
		}
	} else {
		for i := 0; i < len(pvec); i++ {
			builder.AddEdge(pvec[i], pvec[(i+1)%len(pvec)])
		}
		var poly s2.Polygon
		builder.AssemblePolygon(&poly, nil)
		covering = coverer.Covering(&poly)
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

type setGeoms struct {
	Geoms *geojson.FeatureCollection `json:"geoms"`
}

type SetOperationFn func(a, b, c *s2.Polygon) error

func unionOperation(a, b, c *s2.Polygon) error {
	c.InitToUnion(a, b)
	return nil
}

func intersectionOperation(a, b, c *s2.Polygon) error {
	c.InitToIntersection(a, b)
	return nil
}

func differenceOperation(a, b, c *s2.Polygon) error {
	c.InitToDifference(a, b)
	return nil
}

func (s *setGeoms) BuildPolygon(fn SetOperationFn) (*s2.Polygon, error) {
	if len(s.Geoms.Features) < 2 {
		return nil, errors.New("Need at least two polygons")
	}
	a, err := polygonFromFeature(s.Geoms.Features[0])
	if err != nil {
		return nil, err
	}
	for i := 1; i < len(s.Geoms.Features); i++ {
		var c s2.Polygon
		b, err := polygonFromFeature(s.Geoms.Features[i])
		if err != nil {
			return nil, err
		}
		// TODO: If "a" and "b" are disjoint, I imagine "c" will have zero loops...
		// TODO: that means your return value will have multiple polygons.
		fn(a, b, &c)
		a = &c
	}
	return a, nil
}

func polygonFromFeature(feat *geojson.Feature) (*s2.Polygon, error) {
	geom, err := feat.GetGeometry()
	if err != nil {
		return nil, err
	}
	poly := geom.(*geojson.Polygon)
	var v []float64
	for _, coord := range poly.Coordinates[0] {
		v = append(v, float64(coord[0]))
		v = append(v, float64(coord[1]))
	}
	var points []s2.Point
	for i := 0; i < len(v); i += 2 {
		ll := s2.LatLngFromDegrees(v[i+1], v[i])
		points = append(points, s2.PointFromLatLng(ll))
	}
	builder := s2.NewPolygonBuilder(s2.DIRECTED_XOR())
	for i := 0; i < len(points); i++ {
		builder.AddEdge(points[i], points[(i+1)%len(points)])
	}
	var out s2.Polygon
	builder.AssemblePolygon(&out, nil)
	return &out, nil
}

func polygonToGeoJson(poly *s2.Polygon) *geojson.Polygon {
	var coordinates []geojson.Coordinates
	for i := 0; i < poly.NumLoops(); i++ {
		loop := poly.Loop(i)
		var coords geojson.Coordinates
		// +1 to close loop.
		for j := 0; j < loop.NumVertices()+1; j++ {
			ll := s2.LatLngFromPoint(*loop.Vertex(j))
			lat := geojson.CoordType(ll.Lat.Degrees())
			lng := geojson.CoordType(ll.Lng.Degrees())
			coords = append(coords, geojson.Coordinate{lng, lat})
		}
		coordinates = append(coordinates, coords)
	}
	return geojson.NewPolygon(geojson.MultiLine(coordinates))
}

func union(w http.ResponseWriter, r *http.Request) {
	t0 := time.Now()
	w.Header().Set("Content-Type", "application/json")
	decoder := json.NewDecoder(r.Body)
	var geoms setGeoms
	err := decoder.Decode(&geoms)
	if hasError(w, err) {
		return
	}
	a, err := geoms.BuildPolygon(unionOperation)
	if hasError(w, err) {
		return
	}
	poly := polygonToGeoJson(a)
	feat := geojson.NewFeature(poly, nil, nil)
	collection := geojson.NewFeatureCollection([]*geojson.Feature{feat})
	enc := json.NewEncoder(w)
	if err := enc.Encode(collection); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
	}
	log.Println("Request took:", time.Since(t0))
}

func intersection(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	decoder := json.NewDecoder(r.Body)
	var geoms setGeoms
	err := decoder.Decode(&geoms)
	if hasError(w, err) {
		return
	}
	a, err := geoms.BuildPolygon(intersectionOperation)
	if hasError(w, err) {
		return
	}
	poly := polygonToGeoJson(a)
	feat := geojson.NewFeature(poly, nil, nil)
	collection := geojson.NewFeatureCollection([]*geojson.Feature{feat})
	enc := json.NewEncoder(w)
	if err := enc.Encode(collection); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
	}
}

func difference(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	decoder := json.NewDecoder(r.Body)
	var geoms setGeoms
	err := decoder.Decode(&geoms)
	if hasError(w, err) {
		return
	}
	a, err := geoms.BuildPolygon(differenceOperation)
	if hasError(w, err) {
		return
	}
	poly := polygonToGeoJson(a)
	feat := geojson.NewFeature(poly, nil, nil)
	collection := geojson.NewFeatureCollection([]*geojson.Feature{feat})
	enc := json.NewEncoder(w)
	if err := enc.Encode(collection); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
	}
}

func init() {
	r := mux.NewRouter()
	r.HandleFunc("/", IndexHandler)
	r.HandleFunc("/a/s2cover", S2CoverHandler)
	r.HandleFunc("/a/union", union)
	r.HandleFunc("/a/intersection", intersection)
	r.HandleFunc("/a/difference", difference)
	http.Handle("/", r)
}
