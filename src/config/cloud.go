package variable

import "os"

var (
	DefaultCloudAPIURL    = ""
	DefaultGoogleClientID = ""
	DefaultFirebaseAPIKey = ""
)

func CloudAPIURL() string    { return envCloud("WRAPPER_CLOUD_API_URL", DefaultCloudAPIURL) }
func GoogleClientID() string { return envCloud("WRAPPER_GOOGLE_CLIENT_ID", DefaultGoogleClientID) }
func FirebaseAPIKey() string { return envCloud("WRAPPER_FIREBASE_API_KEY", DefaultFirebaseAPIKey) }

func envCloud(name, fallback string) string {
	if value := os.Getenv(name); value != "" {
		return value
	}
	return fallback
}
