package updates

import "time"

type Media struct {
	Src  string `json:"src"`
	Type string `json:"type"`
	Alt  string `json:"alt"`
}

type Post struct {
	ID    string    `json:"id"`
	Text  string    `json:"text"`
	Date  time.Time `json:"date"`
	Media []Media   `json:"media"`
}
