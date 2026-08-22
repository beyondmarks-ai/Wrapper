package control

import (
	"context"
	"strings"

	"cloud.google.com/go/firestore"
	"firebase.google.com/go/v4/auth"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

type FirebaseVerifier struct{ client *auth.Client }

func NewFirebaseVerifier(client *auth.Client) *FirebaseVerifier {
	return &FirebaseVerifier{client: client}
}

func (v *FirebaseVerifier) Verify(ctx context.Context, idToken string) (User, error) {
	token, err := v.client.VerifyIDTokenAndCheckRevoked(ctx, idToken)
	if err != nil {
		return User{}, err
	}
	email, _ := token.Claims["email"].(string)
	return User{ID: token.UID, Email: strings.ToLower(strings.TrimSpace(email))}, nil
}

type FirestoreInvites struct{ client *firestore.Client }

func NewFirestoreInvites(client *firestore.Client) *FirestoreInvites {
	return &FirestoreInvites{client: client}
}

func (i *FirestoreInvites) Allowed(ctx context.Context, user User) (bool, error) {
	snapshot, err := i.client.Collection("betaInvites").Doc(user.ID).Get(ctx)
	if status.Code(err) == codes.NotFound {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	enabled, _ := snapshot.Data()["enabled"].(bool)
	return enabled, nil
}
