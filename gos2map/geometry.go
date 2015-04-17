package gos2map

import (
	"github.com/davidreynolds/geojson"
	"github.com/davidreynolds/gos2/s2"
)

func geometryToS2Polygon(geom geojson.GeoJSON) (*s2.Polygon, error) {
	var poly *s2.Polygon
	builder := s2.NewPolygonBuilder(s2.DIRECTED_XOR())
	switch geom := geom.(type) {
	case geojson.Polygon:
		for _, ring := range geom.Coordinates {
			var points []s2.Point
			for _, v := range ring {
				points = append(points, s2.PointFromLatLng(s2.LatLngFromDegrees(v[1], v[0])))
			}
			builder.AddLoop(s2.NewLoopFromPath(points))
		}
		poly = new(s2.Polygon)
		builder.AssemblePolygon(poly, nil)
	}
	return poly, nil
}

func s2PolygonToGeometry(poly s2.Polygon) *geojson.Polygon {
	// Don't want nil rings for coordinates.
	rings := [][]geojson.Coordinate{}
	for i := 0; i < poly.NumLoops(); i++ {
		loop := poly.Loop(i)
		var ring []geojson.Coordinate
		for j := 0; j <= loop.NumVertices(); j++ {
			ll := s2.LatLngFromPoint(*loop.Vertex(j))
			ring = append(ring, geojson.Coordinate{ll.Lng.Degrees(), ll.Lat.Degrees()})
		}
		rings = append(rings, ring)
	}
	return &geojson.Polygon{Typ: "Polygon", Coordinates: rings}
}

func geometryToPolygonList(js geojson.GeoJSON) ([]*s2.Polygon, error) {
	var polygons []*s2.Polygon
	switch js := js.(type) {
	case geojson.FeatureCollection:
		for _, feature := range js.Features {
			geom := feature.Geometry
			poly, err := geometryToS2Polygon(geom)
			if err != nil {
				return nil, err
			}
			if poly != nil {
				polygons = append(polygons, poly)
			}
		}
	}
	return polygons, nil
}

func featureCollectionFromS2Polygon(poly *s2.Polygon) *geojson.FeatureCollection {
	feat := geojson.Feature{
		Typ:      "Feature",
		Geometry: s2PolygonToGeometry(*poly),
	}
	fc := &geojson.FeatureCollection{
		Typ:      "FeatureCollection",
		Features: []geojson.Feature{feat},
	}
	return fc
}

func Union(js geojson.GeoJSON) (*geojson.FeatureCollection, error) {
	polygons, err := geometryToPolygonList(js)
	if err != nil {
		return nil, err
	}
	a := polygons[0]
	for i := 1; i < len(polygons); i++ {
		var c s2.Polygon
		b := polygons[i]
		c.InitToUnion(a, b)
		a = &c
	}
	return featureCollectionFromS2Polygon(a), nil
}

func Intersection(js geojson.GeoJSON) (*geojson.FeatureCollection, error) {
	polygons, err := geometryToPolygonList(js)
	if err != nil {
		return nil, err
	}
	var intersections []*s2.Polygon
	for i := 0; i < len(polygons); i++ {
		a := polygons[i]
		for j := i + 1; j < len(polygons); j++ {
			var c s2.Polygon
			b := polygons[j]
			c.InitToIntersection(a, b)
			intersections = append(intersections, &c)
		}
	}
	a := intersections[0]
	for i := 1; i < len(intersections); i++ {
		b := intersections[i]
		var c s2.Polygon
		c.InitToUnion(a, b)
		a = &c
	}
	return featureCollectionFromS2Polygon(a), nil
}

func Difference(js geojson.GeoJSON) (*geojson.FeatureCollection, error) {
	polygons, err := geometryToPolygonList(js)
	if err != nil {
		return nil, err
	}
	a := polygons[0]
	for i := 1; i < len(polygons); i++ {
		var c s2.Polygon
		b := polygons[i]
		c.InitToDifference(a, b)
		a = &c
	}
	return featureCollectionFromS2Polygon(a), nil
}

func SymmetricDifference(js geojson.GeoJSON) (*geojson.FeatureCollection, error) {
	polygons, err := geometryToPolygonList(js)
	if err != nil {
		return nil, err
	}
	var features []geojson.Feature
	for i := 0; i < len(polygons); i++ {
		a := polygons[i]
		for j := 0; j < len(polygons); j++ {
			if i == j {
				continue
			}
			var c s2.Polygon
			b := polygons[j]
			c.InitToDifference(a, b)
			a = &c
		}
		if a.NumLoops() > 0 {
			feat := geojson.Feature{
				Typ:      "Feature",
				Geometry: s2PolygonToGeometry(*a),
			}
			features = append(features, feat)
		}
	}
	fc := &geojson.FeatureCollection{
		Typ:      "FeatureCollection",
		Features: features,
	}
	return fc, nil
}
