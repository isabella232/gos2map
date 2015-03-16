package gos2map

import (
	"github.com/davidreynolds/gos2/s2"
	"github.com/kpawlik/geojson"
)

type FeatureCollection geojson.FeatureCollection

func (fc *FeatureCollection) Union() ([]*geojson.Feature, error) {
	a, err := polygonFromFeature(fc.Features[0])
	if err != nil {
		return nil, err
	}
	for i := 1; i < len(fc.Features); i++ {
		var c s2.Polygon
		b, err := polygonFromFeature(fc.Features[i])
		if err != nil {
			return nil, err
		}
		c.InitToUnion(a, b)
		a = &c
	}
	feat := polygonToFeature(a)
	return []*geojson.Feature{feat}, nil
}

func (fc *FeatureCollection) Intersection() ([]*geojson.Feature, error) {
	var out []*s2.Polygon
	for i := 0; i < len(fc.Features); i++ {
		a, err := polygonFromFeature(fc.Features[i])
		if err != nil {
			return nil, err
		}
		for j := i + 1; j < len(fc.Features); j++ {
			var c s2.Polygon
			b, err := polygonFromFeature(fc.Features[j])
			if err != nil {
				return nil, err
			}
			c.InitToIntersection(a, b)
			out = append(out, &c)
		}
	}
	a := out[0]
	for i := 1; i < len(out); i++ {
		b := out[i]
		var c s2.Polygon
		c.InitToUnion(a, b)
		a = &c
	}
	feat := polygonToFeature(a)
	return []*geojson.Feature{feat}, nil
}

func (fc *FeatureCollection) Difference() ([]*geojson.Feature, error) {
	a, err := polygonFromFeature(fc.Features[0])
	if err != nil {
		return nil, err
	}
	for i := 1; i < len(fc.Features); i++ {
		var c s2.Polygon
		b, err := polygonFromFeature(fc.Features[i])
		if err != nil {
			return nil, err
		}
		c.InitToDifference(a, b)
		a = &c
	}
	feat := polygonToFeature(a)
	return []*geojson.Feature{feat}, nil
}

func (fc *FeatureCollection) SymmetricDifference() ([]*geojson.Feature, error) {
	var out []*geojson.Feature
	for i := 0; i < len(fc.Features); i++ {
		a, err := polygonFromFeature(fc.Features[i])
		if err != nil {
			return nil, err
		}
		for j := 0; j < len(fc.Features); j++ {
			if i == j {
				continue
			}
			var c s2.Polygon
			b, err := polygonFromFeature(fc.Features[j])
			if err != nil {
				return nil, err
			}
			c.InitToDifference(a, b)
			a = &c
		}
		if a.NumLoops() > 0 {
			feat := polygonToFeature(a)
			out = append(out, feat)
		}
	}
	return out, nil
}

func polygonFromFeature(feat *geojson.Feature) (*s2.Polygon, error) {
	builder := s2.NewPolygonBuilder(s2.DIRECTED_XOR())
	geom, err := feat.GetGeometry()
	if err != nil {
		return nil, err
	}
	poly := geom.(*geojson.Polygon)
	for i := 0; i < len(poly.Coordinates); i++ {
		var v []float64
		var points []s2.Point
		for _, coord := range poly.Coordinates[i] {
			v = append(v, float64(coord[0]))
			v = append(v, float64(coord[1]))
		}
		for j := 0; j < len(v); j += 2 {
			ll := s2.LatLngFromDegrees(v[j+1], v[j])
			points = append(points, s2.PointFromLatLng(ll))
		}
		for j := 0; j < len(points); j++ {
			builder.AddEdge(points[j], points[(j+1)%len(points)])
		}
	}
	var out s2.Polygon
	builder.AssemblePolygon(&out, nil)
	return &out, nil
}

func polygonToFeature(poly *s2.Polygon) *geojson.Feature {
	var coordinates []geojson.Coordinates
	for i := 0; i < poly.NumLoops(); i++ {
		loop := poly.Loop(i)
		var coords geojson.Coordinates
		for j := 0; j <= loop.NumVertices(); j++ {
			ll := s2.LatLngFromPoint(*loop.Vertex(j))
			lat := geojson.CoordType(ll.Lat.Degrees())
			lng := geojson.CoordType(ll.Lng.Degrees())
			coords = append(coords, geojson.Coordinate{lng, lat})
		}
		coordinates = append(coordinates, coords)
	}
	featPoly := geojson.NewPolygon(geojson.MultiLine(coordinates))
	return geojson.NewFeature(featPoly, nil, nil)
}
