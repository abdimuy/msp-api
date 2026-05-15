package firebase

// TokenVerifierForTest mirrors the internal tokenVerifier interface so
// black-box tests in package firebase_test can supply fakes.
type TokenVerifierForTest = tokenVerifier

// UserManagerForTest mirrors the internal userManager interface so
// black-box tests in package firebase_test can supply fakes.
type UserManagerForTest = userManager

// ExportNewRealClientForTest exposes the unexported constructor used by
// black-box unit tests in package firebase_test.
func ExportNewRealClientForTest(v TokenVerifierForTest, projectID string) *RealClient {
	return newRealClientWithVerifier(v, projectID)
}

// ExportNewRealClientWithDepsForTest exposes the unexported constructor
// that also takes a user manager. Used by admin-path tests.
func ExportNewRealClientWithDepsForTest(v TokenVerifierForTest, u UserManagerForTest, projectID string) *RealClient {
	return newRealClientWithDeps(v, u, projectID)
}
