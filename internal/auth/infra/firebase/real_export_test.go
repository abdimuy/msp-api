package firebase

// TokenVerifierForTest mirrors the internal tokenVerifier interface so
// black-box tests in package firebase_test can supply fakes.
type TokenVerifierForTest = tokenVerifier

// ExportNewRealClientForTest exposes the unexported constructor used by
// black-box unit tests in package firebase_test.
func ExportNewRealClientForTest(v TokenVerifierForTest, projectID string) *RealClient {
	return newRealClientWithVerifier(v, projectID)
}
