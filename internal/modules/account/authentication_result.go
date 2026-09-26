package account

import "github.com/google/uuid"

func sessionCallback(session SessionCredentials) CallbackResult {
	return CallbackResult{result: ResultSession, session: session}
}

func reauthenticatedCallback(id uuid.UUID) CallbackResult {
	return CallbackResult{result: ResultReauthenticated, reauthenticationID: id}
}

func linkPendingCallback(link LinkConfirmation) CallbackResult {
	return CallbackResult{result: ResultLinkPending, link: link}
}

func redirectReauthentication(flow FlowResult) ReauthenticationResult {
	return ReauthenticationResult{result: ResultRedirect, flow: flow}
}

func completedReauthentication(id uuid.UUID) ReauthenticationResult {
	return ReauthenticationResult{result: ResultReauthenticated, reauthenticationID: id}
}

func (r CallbackResult) Result() AuthResult { return r.result }

func (r CallbackResult) Session() (SessionCredentials, bool) {
	return r.session, r.result == ResultSession
}

func (r CallbackResult) Link() (LinkConfirmation, bool) {
	return r.link, r.result == ResultLinkPending
}

// ReauthenticationID 仅在重新认证完成后返回操作证明 ID，否则返回 uuid.Nil。
func (r CallbackResult) ReauthenticationID() uuid.UUID { return r.reauthenticationID }

func (r ReauthenticationResult) Result() AuthResult { return r.result }

func (r ReauthenticationResult) Flow() (FlowResult, bool) {
	return r.flow, r.result == ResultRedirect
}

// ReauthenticationID 仅在重新认证完成后返回操作证明 ID，否则返回 uuid.Nil。
func (r ReauthenticationResult) ReauthenticationID() uuid.UUID { return r.reauthenticationID }
