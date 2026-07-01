package main

import (
	"bytes"
	"encoding/binary"
	"image"
	"image/color"
	"image/jpeg"
	"mime/multipart"
	"net/http"
	"regexp"
	"strings"
	"testing"
	"time"
)

func TestFarmersMarketEndToEndUploadValidation(t *testing.T) {
	srv := newTestServer(t)
	defer srv.Close()

	client := newTestClient(t)
	body := mustGetBody(t, client, srv.URL+"/farmersmarket")
	for _, want := range []string{
		"Farmers market finds",
		`id="farmers-market-error"`,
		`hx-post="/farmersmarket"`,
	} {
		if !strings.Contains(body, want) {
			t.Fatalf("expected farmers market page to contain %q, got body: %s", want, body)
		}
	}

	respBody, headers := mustPostMultipartHTMX(t, client, srv.URL+"/farmersmarket", map[string]string{
		"name": "Test Market",
	}, "photos", "market.jpg", jpegBytes(t))
	if headers.Get("HX-Retarget") != "#farmers-market-error" {
		t.Fatalf("expected HX-Retarget to #farmers-market-error, got %q", headers.Get("HX-Retarget"))
	}
	if headers.Get("HX-Reswap") != "outerHTML" {
		t.Fatalf("expected HX-Reswap to outerHTML, got %q", headers.Get("HX-Reswap"))
	}
	for _, want := range []string{
		`id="farmers-market-error"`,
		"could not read location",
	} {
		if !strings.Contains(respBody, want) {
			t.Fatalf("expected farmers market upload response to contain %q, got body: %s", want, respBody)
		}
	}
}

func TestFarmersMarketEndToEndSuccessfulUploadRedirectsToRecipes(t *testing.T) {
	srv := newTestServer(t)
	defer srv.Close()

	client := newTestClient(t)
	progressBody, _ := mustPostMultipartHTMX(t, client, srv.URL+"/farmersmarket", map[string]string{
		"name": "Test Market",
	}, "photos", "market.jpg", geotaggedJPEGBytes(t))
	for _, want := range []string{
		`id="farmers-market-work"`,
		`hx-get="/farmersmarket/status/`,
		"Looking through your market photos",
	} {
		if !strings.Contains(progressBody, want) {
			t.Fatalf("expected successful upload response to contain %q, got body: %s", want, progressBody)
		}
	}

	statusPath := extractFarmersMarketStatusPath(t, progressBody)
	redirect := waitForFarmersMarketRedirect(t, client, srv.URL+statusPath)
	if !strings.HasPrefix(redirect, "/recipes?") {
		t.Fatalf("expected farmers market upload to redirect to recipes, got %q", redirect)
	}
	if !strings.Contains(redirect, "location=farmersmarket_") {
		t.Fatalf("expected farmers market location redirect, got %q", redirect)
	}
}

func mustPostMultipartHTMX(t *testing.T, client *http.Client, targetURL string, fields map[string]string, fileField, fileName string, fileData []byte) (string, http.Header) {
	t.Helper()
	var body bytes.Buffer
	writer := multipart.NewWriter(&body)
	for name, value := range fields {
		if err := writer.WriteField(name, value); err != nil {
			t.Fatalf("failed to write multipart field %q: %v", name, err)
		}
	}
	part, err := writer.CreateFormFile(fileField, fileName)
	if err != nil {
		t.Fatalf("failed to create multipart file field: %v", err)
	}
	if _, err := part.Write(fileData); err != nil {
		t.Fatalf("failed to write multipart file data: %v", err)
	}
	if err := writer.Close(); err != nil {
		t.Fatalf("failed to close multipart writer: %v", err)
	}

	req, err := http.NewRequest(http.MethodPost, targetURL, &body)
	if err != nil {
		t.Fatalf("POST %s failed to build request: %v", targetURL, err)
	}
	req.Header.Set("Content-Type", writer.FormDataContentType())
	req.Header.Set("HX-Request", "true")
	resp, err := client.Do(req)
	if err != nil {
		t.Fatalf("POST %s failed: %v", targetURL, err)
	}
	defer func() {
		if err := resp.Body.Close(); err != nil {
			t.Fatalf("failed to close response body: %v", err)
		}
	}()
	if resp.StatusCode != http.StatusOK {
		respBody := readAll(t, resp.Body)
		t.Fatalf("POST %s expected 200, got %d: %s", targetURL, resp.StatusCode, respBody)
	}
	return readAll(t, resp.Body), resp.Header.Clone()
}

func extractFarmersMarketStatusPath(t *testing.T, body string) string {
	t.Helper()
	matches := regexp.MustCompile(`hx-get="(/farmersmarket/status/[^"]+)"`).FindStringSubmatch(body)
	if len(matches) != 2 {
		t.Fatalf("expected farmers market progress body to include status hx-get, got body: %s", body)
	}
	return matches[1]
}

func waitForFarmersMarketRedirect(t *testing.T, client *http.Client, statusURL string) string {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for {
		if time.Now().After(deadline) {
			t.Fatalf("timed out waiting for farmers market redirect from %s", statusURL)
		}
		req, err := http.NewRequest(http.MethodGet, statusURL, nil)
		if err != nil {
			t.Fatalf("GET %s failed to build request: %v", statusURL, err)
		}
		req.Header.Set("HX-Request", "true")
		resp, err := client.Do(req)
		if err != nil {
			t.Fatalf("GET %s failed: %v", statusURL, err)
		}
		body := readAll(t, resp.Body)
		if err := resp.Body.Close(); err != nil {
			t.Fatalf("failed to close response body: %v", err)
		}
		if resp.StatusCode != http.StatusOK {
			t.Fatalf("GET %s expected 200, got %d: %s", statusURL, resp.StatusCode, body)
		}
		if redirect := resp.Header.Get("HX-Redirect"); redirect != "" {
			return redirect
		}
		time.Sleep(25 * time.Millisecond)
	}
}

func geotaggedJPEGBytes(t *testing.T) []byte {
	t.Helper()
	return addGPSExif(t, jpegBytes(t))
}

func jpegBytes(t *testing.T) []byte {
	t.Helper()
	img := image.NewRGBA(image.Rect(0, 0, 2, 2))
	img.Set(0, 0, color.White)
	var b bytes.Buffer
	if err := jpeg.Encode(&b, img, nil); err != nil {
		t.Fatalf("failed to encode jpeg: %v", err)
	}
	return b.Bytes()
}

func addGPSExif(t *testing.T, jpg []byte) []byte {
	t.Helper()
	if len(jpg) < 2 || jpg[0] != 0xff || jpg[1] != 0xd8 {
		t.Fatalf("expected jpeg data to start with SOI marker")
	}

	tiff := make([]byte, 128)
	copy(tiff[0:], []byte{'I', 'I'})
	binary.LittleEndian.PutUint16(tiff[2:], 42)
	binary.LittleEndian.PutUint32(tiff[4:], 8)

	const (
		ifd0Offset = 8
		gpsOffset  = 26
		latOffset  = 80
		lonOffset  = 104
	)
	binary.LittleEndian.PutUint16(tiff[ifd0Offset:], 1)
	putIFDEntry(tiff, ifd0Offset+2, 0x8825, 4, 1, gpsOffset)

	binary.LittleEndian.PutUint16(tiff[gpsOffset:], 4)
	putASCIIIFDEntry(tiff, gpsOffset+2, 1, "N")
	putIFDEntry(tiff, gpsOffset+14, 2, 5, 3, latOffset)
	putASCIIIFDEntry(tiff, gpsOffset+26, 3, "W")
	putIFDEntry(tiff, gpsOffset+38, 4, 5, 3, lonOffset)

	putDMSRationals(tiff[latOffset:], 47, 36, 36)
	putDMSRationals(tiff[lonOffset:], 122, 19, 48)

	payload := append([]byte("Exif\x00\x00"), tiff...)
	if len(payload)+2 > 0xffff {
		t.Fatalf("exif payload too large")
	}
	app1 := []byte{0xff, 0xe1, byte((len(payload) + 2) >> 8), byte(len(payload) + 2)}
	app1 = append(app1, payload...)

	out := make([]byte, 0, len(jpg)+len(app1))
	out = append(out, jpg[:2]...)
	out = append(out, app1...)
	out = append(out, jpg[2:]...)
	return out
}

func putIFDEntry(tiff []byte, offset int, tag, typ uint16, count, value uint32) {
	binary.LittleEndian.PutUint16(tiff[offset:], tag)
	binary.LittleEndian.PutUint16(tiff[offset+2:], typ)
	binary.LittleEndian.PutUint32(tiff[offset+4:], count)
	binary.LittleEndian.PutUint32(tiff[offset+8:], value)
}

func putASCIIIFDEntry(tiff []byte, offset int, tag uint16, value string) {
	binary.LittleEndian.PutUint16(tiff[offset:], tag)
	binary.LittleEndian.PutUint16(tiff[offset+2:], 2)
	binary.LittleEndian.PutUint32(tiff[offset+4:], 2)
	tiff[offset+8] = value[0]
	tiff[offset+9] = 0
}

func putDMSRationals(dst []byte, degrees, minutes, seconds uint32) {
	values := []uint32{degrees, 1, minutes, 1, seconds, 1}
	for i, value := range values {
		binary.LittleEndian.PutUint32(dst[i*4:], value)
	}
}
