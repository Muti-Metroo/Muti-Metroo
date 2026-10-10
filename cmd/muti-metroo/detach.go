package main

// breakawayServiceHint returns the install suggestion appended to the Windows
// detach failure that fires when the OpenSSH Job Object forbids breakaway. The
// suggestion depends on privilege: a non-Administrator cannot install a system
// service, so point them at the no-admin per-user (Registry Run key) install
// instead of the generic system install.
//
// It lives in a build-tag-free file so it can be unit-tested on any OS even
// though detachSelf (its only caller) compiles on Windows only.
func breakawayServiceHint(isAdmin bool) string {
	if isAdmin {
		return "install muti-metroo as a system service instead (muti-metroo service install -c <config>)"
	}
	return "you are not an Administrator, so install muti-metroo as a per-user service instead " +
		"(muti-metroo service install --user --dll <path-to-muti-metroo.dll> -c <config>)"
}
