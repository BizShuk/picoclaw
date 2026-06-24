package agent

// ParentTurnState returns the parent turnState (nil if root).
func (ts *turnState) ParentTurnState() *turnState {
	ts.mu.RLock()
	defer ts.mu.RUnlock()
	return ts.parentTurnState
}

// SessionKey returns the session key of the turnState.
func (ts *turnState) SessionKey() string {
	ts.mu.RLock()
	defer ts.mu.RUnlock()
	return ts.sessionKey
}
