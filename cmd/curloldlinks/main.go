package main

import (
	"bufio"
	"errors"
	"flag"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"regexp"
	"slices"
	"strings"
	"time"
)

var urlPattern = regexp.MustCompile(`https?://[^\s<>"']+`)

func main() {
	var (
		inputFile = flag.String("file", "oldiniks.txt", "Input file with URLs")
		target    = flag.String("target", "localhost:8080", "Target host:port")
	)
	flag.Parse()

	file, usedPath, err := openInput(*inputFile)
	if err != nil {
		fmt.Fprintf(os.Stderr, "open input: %v\n", err)
		os.Exit(1)
	}
	defer func() { _ = file.Close() }()

	fmt.Printf("reading %s\n", usedPath)

	scanner := bufio.NewScanner(file)
	seen := make(map[string]struct{})
	total := 0
	client := &http.Client{Timeout: 15 * time.Second}
	statusCounts := make(map[int]int)
	requestErrors := 0

	for scanner.Scan() {
		line := scanner.Text()
		for _, rawURL := range urlPattern.FindAllString(line, -1) {
			rewritten, ok := rewrite(rawURL, *target)
			if !ok {
				continue
			}
			if _, exists := seen[rewritten]; exists {
				continue
			}
			seen[rewritten] = struct{}{}
			total++

			code, err := getStatus(client, rewritten)
			if err != nil {
				fmt.Printf("ERR %s (%v)\n", rewritten, err)
				requestErrors++
				continue
			}
			fmt.Printf("%d %s\n", code, rewritten)
			statusCounts[code]++
		}
	}
	if err := scanner.Err(); err != nil {
		fmt.Fprintf(os.Stderr, "scan input: %v\n", err)
		os.Exit(1)
	}

	codes := make([]int, 0, len(statusCounts))
	for code := range statusCounts {
		codes = append(codes, code)
	}
	slices.Sort(codes)

	fmt.Println("summary:")
	for _, code := range codes {
		fmt.Printf("%d: %d\n", code, statusCounts[code])
	}
	if requestErrors > 0 {
		fmt.Printf("ERR: %d\n", requestErrors)
	}
	fmt.Printf("done: %d unique urls\n", total)
}

func openInput(path string) (*os.File, string, error) {
	f, err := os.Open(path)
	if err == nil {
		return f, path, nil
	}
	if !errors.Is(err, os.ErrNotExist) || path != "oldiniks.txt" {
		return nil, "", err
	}

	// Graceful fallback for common typo vs existing repo filename.
	fallback := "oldlinks.txt"
	f, err = os.Open(fallback)
	if err != nil {
		return nil, "", err
	}
	return f, fallback, nil
}

func rewrite(rawURL string, targetHost string) (string, bool) {
	u, err := url.Parse(rawURL)
	if err != nil {
		return "", false
	}

	if !strings.EqualFold(u.Host, "careme.cooking") {
		return "", false
	}

	u.Scheme = "http"
	u.Host = targetHost
	return u.String(), true
}

func getStatus(client *http.Client, rawURL string) (int, error) {
	res, err := client.Get(rawURL)
	if err != nil {
		return 0, err
	}
	defer func() { _ = res.Body.Close() }()
	_, _ = io.Copy(io.Discard, res.Body)

	return res.StatusCode, nil
}

/* old links
summary:
200: 347
404: 109
500: 11
*/

/*new links
200: 347
404: 109
500: 11
*/
