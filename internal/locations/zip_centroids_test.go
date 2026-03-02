package locations

import "testing"

func TestZipCentroidByZIP_KnownZip(t *testing.T) {
	t.Parallel()

	centroids := mustLoadEmbeddedZipCentroids(t)
	centroid, ok := centroids.ZipCentroidByZIP("00601")
	if !ok {
		t.Fatal("expected centroid for 00601")
	}
	if centroid.Lat != 18.180555 {
		t.Fatalf("unexpected latitude: %f", centroid.Lat)
	}
	if centroid.Lon != -66.749961 {
		t.Fatalf("unexpected longitude: %f", centroid.Lon)
	}
}

func TestZipCentroidByZIP_ZipPlus4(t *testing.T) {
	t.Parallel()

	centroids := mustLoadEmbeddedZipCentroids(t)
	centroid, ok := centroids.ZipCentroidByZIP("00601-1234")
	if !ok {
		t.Fatal("expected centroid for ZIP+4")
	}
	if centroid.Lat != 18.180555 || centroid.Lon != -66.749961 {
		t.Fatalf("unexpected centroid: %+v", centroid)
	}
}

func TestZipCentroidByZIP_Unknown(t *testing.T) {
	t.Parallel()

	centroids := mustLoadEmbeddedZipCentroids(t)
	_, ok := centroids.ZipCentroidByZIP("00000")
	if ok {
		t.Fatal("expected no centroid for unknown zip")
	}
}

func TestZipCentroidDataLoaded(t *testing.T) {
	t.Parallel()

	centroids := mustLoadEmbeddedZipCentroids(t)
	if centroids.Len() < 30000 {
		t.Fatalf("expected large centroid dataset, got %d", centroids.Len())
	}
}

func TestNearestZIPToCoordinates(t *testing.T) {
	t.Parallel()

	centroids := newZipCentroidIndex(map[string]ZipCentroid{
		"10001": {Lat: 40.7506, Lon: -73.9972},
		"94105": {Lat: 37.7898, Lon: -122.3942},
		"98101": {Lat: 47.6105, Lon: -122.3348},
	})

	zip, ok := centroids.NearestZIPToCoordinates(47.6097, -122.3331)
	if !ok {
		t.Fatal("expected nearest ZIP for valid coordinates")
	}
	if zip != "98101" {
		t.Fatalf("unexpected nearest ZIP: got %q want %q", zip, "98101")
	}
}

func TestNearestZIPToCoordinates_EmptyCentroids(t *testing.T) {
	t.Parallel()

	zip, ok := newZipCentroidIndex(map[string]ZipCentroid{}).NearestZIPToCoordinates(47.6097, -122.3331)
	if ok {
		t.Fatalf("expected no nearest ZIP, got %q", zip)
	}
}

func mustLoadEmbeddedZipCentroids(t *testing.T) zipCentroidIndex {
	t.Helper()

	centroids, err := loadEmbeddedZipCentroids()
	if err != nil {
		t.Fatalf("loadEmbeddedZipCentroids error: %v", err)
	}
	return centroids
}
