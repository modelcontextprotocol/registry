package commands

import "time"

// SetPublishRetryClock replaces the sleep and clock used by the publish
// --wait retry loop so tests do not wait for real. The returned function
// restores the originals.
func SetPublishRetryClock(sleep func(time.Duration), now func() time.Time) func() {
	origSleep, origNow := publishRetrySleep, publishRetryNow
	publishRetrySleep, publishRetryNow = sleep, now
	return func() {
		publishRetrySleep, publishRetryNow = origSleep, origNow
	}
}

// SetPublishRequestGrace replaces the grace added to the --wait deadline for
// each request. The returned function restores the original.
func SetPublishRequestGrace(grace time.Duration) func() {
	orig := publishRequestGrace
	publishRequestGrace = grace
	return func() { publishRequestGrace = orig }
}

// ClassifyResponse runs the --wait classifier on a failed publish response.
func ClassifyResponse(status int, body []byte) (string, bool) {
	return notVisibleYetMessage(&publishError{StatusCode: status, Body: body})
}

// NotVisibleYetPhrases returns the wordings the --wait classifier retries on.
func NotVisibleYetPhrases() []string {
	return notVisibleYetPhrases
}
