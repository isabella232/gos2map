package gos2map

import (
	"encoding/json"
	"html/template"
	"net/http"
	"strconv"
	"strings"

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

func hasError(w http.ResponseWriter, err error) bool {
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return true
	}
	return false
}

type setGeoms struct {
	A geojson.Feature `json:"a"`
	B geojson.Feature `json:"b"`
}

func polygonFromFeature(feat geojson.Feature) (*s2.Polygon, error) {
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

func buildTwoPolygons(geoms setGeoms) (*s2.Polygon, *s2.Polygon, error) {
	a, err := polygonFromFeature(geoms.A)
	if err != nil {
		return nil, nil, err
	}
	b, err := polygonFromFeature(geoms.B)
	if err != nil {
		return nil, nil, err
	}
	return a, b, nil
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
	w.Header().Set("Content-Type", "application/json")
	decoder := json.NewDecoder(r.Body)
	var geoms setGeoms
	err := decoder.Decode(&geoms)
	if hasError(w, err) {
		return
	}

	a, b, err := buildTwoPolygons(geoms)
	if hasError(w, err) {
		return
	}
	var c s2.Polygon
	c.InitToUnion(a, b)
	poly := polygonToGeoJson(&c)
	feat := geojson.NewFeature(poly, nil, nil)
	js, err := geojson.Marshal(feat)
	if hasError(w, err) {
		return
	}

	w.Write([]byte(js))
}

func intersection(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	decoder := json.NewDecoder(r.Body)
	var geoms setGeoms
	err := decoder.Decode(&geoms)
	if hasError(w, err) {
		return
	}

	a, b, err := buildTwoPolygons(geoms)
	if hasError(w, err) {
		return
	}
	var c s2.Polygon
	c.InitToIntersection(a, b)
	poly := polygonToGeoJson(&c)
	feat := geojson.NewFeature(poly, nil, nil)
	js, err := geojson.Marshal(feat)
	if hasError(w, err) {
		return
	}

	w.Write([]byte(js))
}

func difference(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	decoder := json.NewDecoder(r.Body)
	var geoms setGeoms
	err := decoder.Decode(&geoms)
	if hasError(w, err) {
		return
	}

	a, b, err := buildTwoPolygons(geoms)
	if hasError(w, err) {
		return
	}
	var c s2.Polygon
	c.InitToDifference(a, b)
	poly := polygonToGeoJson(&c)
	feat := geojson.NewFeature(poly, nil, nil)
	js, err := geojson.Marshal(feat)
	if hasError(w, err) {
		return
	}

	w.Write([]byte(js))
}

func init() {
	r := mux.NewRouter()
	r.HandleFunc("/", IndexHandler)
	r.HandleFunc("/api/s2cover", S2CoverHandler)
	r.HandleFunc("/a/union", union)
	r.HandleFunc("/a/intersection", intersection)
	r.HandleFunc("/a/difference", difference)
	http.Handle("/", r)
}
