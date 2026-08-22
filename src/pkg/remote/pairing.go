package remote

import "time"

type Pairing struct {
	ID              string    `json:"id" firestore:"id"`
	UserID          string    `json:"-" firestore:"userId"`
	SourceDevice    string    `json:"sourceDevice" firestore:"sourceDevice"`
	TargetDevice    string    `json:"targetDevice" firestore:"targetDevice"`
	SourceConfirmed bool      `json:"sourceConfirmed" firestore:"sourceConfirmed"`
	TargetConfirmed bool      `json:"targetConfirmed" firestore:"targetConfirmed"`
	CreatedAt       time.Time `json:"createdAt" firestore:"createdAt"`
	ConfirmedAt     time.Time `json:"confirmedAt,omitempty" firestore:"confirmedAt,omitempty"`
}

func (p Pairing) Active() bool {
	return p.SourceConfirmed && p.TargetConfirmed && !p.ConfirmedAt.IsZero()
}
