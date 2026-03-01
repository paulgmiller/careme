package locations

import "testing"

func TestZipCentroidByZIP_KnownZip(t *testing.T) {
	t.Parallel()

	centroids := mustLoadEmbeddedZipCentroids(t)
	centroid, ok := zipCentroidByZIP("00601", centroids)
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
	centroid, ok := zipCentroidByZIP("00601-1234", centroids)
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
	_, ok := zipCentroidByZIP("00000", centroids)
	if ok {
		t.Fatal("expected no centroid for unknown zip")
	}
}

func TestZipCentroidDataLoaded(t *testing.T) {
	t.Parallel()

	centroids := mustLoadEmbeddedZipCentroids(t)
	if len(centroids) < 30000 {
		t.Fatalf("expected large centroid dataset, got %d", len(centroids))
	}
}

func mustLoadEmbeddedZipCentroids(t *testing.T) map[string]ZipCentroid {
	t.Helper()

	centroids, err := loadEmbeddedZipCentroids()
	if err != nil {
		t.Fatalf("loadEmbeddedZipCentroids error: %v", err)
	}
	return centroids
}
