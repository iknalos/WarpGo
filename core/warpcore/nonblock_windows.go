package warpcore

// The fd-backed tunnel only runs on Android; Windows builds exist so the
// desktop probe tool can share this package.
func setNonblock(fd int) error { return nil }
