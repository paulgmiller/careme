package logsink

import "fmt"

// DateFolderFormat is the format string for organizing logs by date in blob storage
// Format: YYYY/MM/DD/
const DateFolderFormat = "%d/%02d/%02d"

// FormatDateFolder returns the date-based folder path for a given year, month, day
func FormatDateFolder(year int, month int, day int) string {
	return fmt.Sprintf(DateFolderFormat, year, month, day)
}
