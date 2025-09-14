package relay

import "time"

type Sponsor struct {
	Contract   string    `json:"contract"`
	PrivateKey string    `json:"private_key"`
	CreatedAt  time.Time `json:"created_at"`
	UpdatedAt  time.Time `json:"updated_at"`
}
