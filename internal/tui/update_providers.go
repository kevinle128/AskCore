package tui

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"strconv"
	"strings"
	"time"

	tea "charm.land/bubbletea/v2"

	"AskCore/pkg/protocol"
)

// Provider flows.
//
// /providers lists the login options, and a chosen row opens a key box, a
// browser or code box, a device box, or a removal box. Every call to the
// leader runs in a command and comes back as a message that carries the id of
// the box and the number of its request, so an answer for a box that is gone or
// replaced changes nothing. A secret is held only by the secret input of the
// box, goes out once in one call, and is dropped on every exit.

const (
	// keySaveTimeout bounds the save of a key, which includes the live check.
	keySaveTimeout = 90 * time.Second
	// loginWaitMax bounds the wait for the end of a browser or device sign-in.
	loginWaitMax = 12 * time.Minute
	// stopCancelTimeout bounds the cancel that a stop signal sends.
	stopCancelTimeout = 2 * time.Second
	// copiedFor is how long the copy result stays on the rail.
	copiedFor = time.Second

	apiKeyMethod = "api-key"
)

// ---- messages ----

type provListedMsg struct {
	gen, req uint64
	res      protocol.ACPProvidersResult
	err      error
}

type loginStartedMsg struct {
	dlg, req uint64
	res      protocol.ACPLoginStartResult
	err      error
}

type keySavedMsg struct {
	dlg, req uint64
	loginID  string
	out      protocol.ACPLoginOutcome
	err      error
}

type loginEndedMsg struct {
	loginID string
	out     protocol.ACPLoginOutcome
	err     error
}

type codeSentMsg struct {
	dlg, req uint64
	loginID  string
	err      error
}

type cancelAnsweredMsg struct {
	loginID   string
	cancelled bool
	err       error
}

type logoutDoneMsg struct {
	dlg, req uint64
	res      protocol.ACPLogoutResult
	err      error
}

type modelsFetchedMsg struct {
	gen, token uint64
	res        protocol.ACPModelsResult
	err        error
}

// closedLogin is a sign-in whose box is gone while the leader has not said how
// it ended. The note that follows comes from the answers, not from the key
// that closed the box.
type closedLogin struct {
	loginID  string
	provider string
	isKey    bool
	// cancelAnswered is true once the answer to the cancel arrived.
	cancelAnswered bool
	cancelled      bool
	cancelErr      error
	outcome        *protocol.ACPLoginOutcome
}

// newLoginID makes the id of a key save. It only has to be unique for this
// client; the leader keys the save on it.
func newLoginID() string {
	var b [8]byte
	if _, err := rand.Read(b[:]); err != nil {
		return "tui-" + strconv.FormatInt(time.Now().UnixNano(), 36)
	}
	return "tui-" + hex.EncodeToString(b[:])
}

// ---- opening and closing ----

// openProviders opens the provider box. It works while a run is active,
// because it does not change the run.
func (s *sessionView) openProviders() tea.Cmd {
	switch {
	case s.opening:
		s.setNotice("Starting, please wait…", false)
		return nil
	case s.info.Mode == ModeInProcess:
		s.setNotice(needsLeaderText+"\nSign in from a shell: ask auth login --provider <id> --method <id>", false)
		return nil
	case s.conn != connUp:
		s.reconnect.hintUntil = s.now().Add(noticeFor)
		return nil
	}
	s.dlg = nil
	s.pbox = &providerBox{loading: true, gen: s.providersGen}
	s.m.SetFocus(FocusDialog)
	return s.listProvidersCmd()
}

// boxOpen tells that a provider box or a login box owns the keys.
func (s *sessionView) boxOpen() bool { return s.pbox != nil || s.dlg != nil }

func (s *sessionView) closeProviders() {
	s.pbox = nil
	if s.dlg == nil {
		s.m.SetFocus(FocusEditor)
	}
}

// closeDialog closes the login box and drops its secret.
func (s *sessionView) closeDialog() {
	if s.dlg != nil {
		s.dlg.secret.reset()
	}
	s.dlg = nil
	if s.pbox == nil {
		s.m.SetFocus(FocusEditor)
	}
}

// dropSecrets clears every secret buffer. The program calls it when it ends.
func (s *sessionView) dropSecrets() {
	if s.dlg != nil {
		s.dlg.secret.reset()
	}
}

func (s *sessionView) listProvidersCmd() tea.Cmd {
	pb := s.pbox
	if pb == nil {
		return nil
	}
	s.provReq++
	pb.req = s.provReq
	gen, req, c := s.gen, pb.req, s.client
	return func() tea.Msg {
		ctx, cancel := callContext()
		defer cancel()
		res, err := c.Providers(ctx)
		return provListedMsg{gen: gen, req: req, res: res, err: err}
	}
}

func (s *sessionView) onProvListed(msg provListedMsg) {
	pb := s.pbox
	if pb == nil || msg.gen != s.gen || msg.req != pb.req {
		return
	}
	if msg.err != nil {
		if errors.Is(msg.err, ErrNeedsLeader) {
			s.closeProviders()
			s.setNotice(needsLeaderText, false)
			return
		}
		reason := errText(msg.err)
		var re *RequestError
		if errors.As(msg.err, &re) && re.Kind == "unsupported" {
			reason = "this leader cannot manage providers"
		}
		if len(pb.items) == 0 {
			pb.loading, pb.loadErr = false, reason
			return
		}
		pb.loading = false
		s.setNotice("Could not refresh providers: "+reason, true)
		return
	}
	if msg.res.Generation < pb.gen {
		return
	}
	pb.gen = msg.res.Generation
	pb.setItems(msg.res.Options)
}

// ---- keys ----

func (s *sessionView) onBoxKey(k tea.KeyPressMsg) tea.Cmd {
	if s.dlg != nil {
		return s.onDialogKey(k)
	}
	return s.onProviderKey(k)
}

// onBoxCtrlC cancels a login or closes a box. It never quits and never counts
// as the first press of the quit confirmation.
func (s *sessionView) onBoxCtrlC() tea.Cmd {
	s.quitUntil = time.Time{}
	if s.dlg != nil {
		return s.dialogEsc()
	}
	if s.pbox != nil && s.pbox.menu != nil {
		s.pbox.menu = nil
		return nil
	}
	s.closeProviders()
	return nil
}

func (s *sessionView) onBoxPaste(msg tea.PasteMsg) tea.Cmd {
	if d := s.dlg; d != nil {
		if d.phase == phaseInput && d.hasInput() {
			d.secret.paste(msg.Content)
			d.refusedText = ""
		}
		return nil
	}
	if s.pbox != nil && s.pbox.menu == nil {
		s.pbox.addFilter(msg.Content)
	}
	return nil
}

func (s *sessionView) onProviderKey(k tea.KeyPressMsg) tea.Cmd {
	pb := s.pbox
	switch pb.key(k) {
	case provClose:
		s.closeProviders()
	case provStart:
		if it, ok := pb.selected(); ok {
			return s.startFlow(it)
		}
	case provMenu:
		return s.startFlow(pb.menu.item)
	case provRemove:
		it := pb.menuOrSelected()
		return s.askRemove(it)
	}
	return nil
}

// menuOrSelected is the row that an action applies to.
func (pb *providerBox) menuOrSelected() provItem {
	if pb.menu != nil {
		return pb.menu.item
	}
	it, _ := pb.selected()
	return it
}

// askRemove opens the removal box, or says why there is nothing to remove.
func (s *sessionView) askRemove(it provItem) tea.Cmd {
	pb := s.pbox
	if s.conn != connUp {
		s.reconnect.hintUntil = s.now().Add(noticeFor)
		return nil
	}
	if !it.saved() {
		pb.menu = nil
		if it.opt.State == protocol.ACPLoginStateEnv {
			pb.notes = []string{
				"Nothing to remove: " + it.opt.ProviderID + " uses " + it.opt.EnvVar + " from the environment.",
				"Unset the variable in your shell to stop using it.",
			}
		} else {
			pb.notes = []string{"Nothing to remove: " + it.opt.ProviderID + " has no saved credential."}
		}
		return nil
	}
	s.pbox = nil
	s.dlg = &loginDialog{id: s.nextDialogID(), opt: it.opt, phase: phaseConfirm, runActive: s.run.running, since: s.now()}
	return nil
}

func (s *sessionView) nextDialogID() uint64 {
	s.dlgSeq++
	return s.dlgSeq
}

// startFlow opens the box of a row and starts its sign-in.
func (s *sessionView) startFlow(it provItem) tea.Cmd {
	if s.conn != connUp {
		s.reconnect.hintUntil = s.now().Add(noticeFor)
		return nil
	}
	d := &loginDialog{id: s.nextDialogID(), opt: it.opt, isKey: it.opt.MethodID == apiKeyMethod, since: s.now()}
	s.pbox = nil
	s.dlg = d
	s.m.SetFocus(FocusDialog)
	if d.isKey {
		d.phase = phaseInput
		return nil
	}
	d.phase = phaseStarting
	return s.startLoginCmd(d)
}

func (s *sessionView) startLoginCmd(d *loginDialog) tea.Cmd {
	d.req++
	d.phase, d.since = phaseStarting, s.now()
	d.loginID, d.url, d.userCode = "", "", ""
	dlg, req, c, opt := d.id, d.req, s.client, d.opt
	return func() tea.Msg {
		ctx, cancel := callContext()
		defer cancel()
		res, err := c.StartLogin(ctx, opt.ProviderID, opt.MethodID)
		return loginStartedMsg{dlg: dlg, req: req, res: res, err: err}
	}
}

func (s *sessionView) onDialogKey(k tea.KeyPressMsg) tea.Cmd {
	d := s.dlg
	key := k.String()
	switch d.phase {
	case phaseStarting:
		if key == "esc" {
			d.cancelAfterStart = true
		}
	case phaseInput, phaseChecking, phaseWaiting:
		if key == "esc" {
			return s.dialogEsc()
		}
		if d.phase == phaseChecking {
			return nil
		}
		if key == "ctrl+y" {
			return s.copyCmd()
		}
		if d.phase != phaseInput || !d.hasInput() {
			return nil
		}
		switch key {
		case "enter":
			return s.sendSecret()
		case "backspace", "ctrl+h":
			d.secret.backspace()
		case "ctrl+u":
			d.secret.reset()
			d.refusedText = ""
		default:
			if k.Text != "" && k.Mod&(tea.ModCtrl|tea.ModAlt) == 0 {
				d.secret.typed(k.Text)
				d.refusedText = ""
			}
		}
	case phaseFailed:
		switch key {
		case "esc":
			return s.dialogEsc()
		case "enter":
			if d.retryable {
				return s.retryFlow()
			}
		}
	case phaseLost:
		if key == "esc" {
			s.closeDialog()
		}
	case phaseConfirm:
		switch key {
		case "esc":
			s.closeDialog()
		case "enter":
			return s.removeCmd()
		}
	}
	return nil
}

// retryFlow starts the sign-in of the box again, with a new login id.
func (s *sessionView) retryFlow() tea.Cmd {
	d := s.dlg
	if s.conn != connUp {
		s.reconnect.hintUntil = s.now().Add(noticeFor)
		return nil
	}
	d.failedText, d.failedNext, d.retryable, d.removed = "", "", false, false
	d.secret.reset()
	return s.startLoginCmd(d)
}

// dialogEsc handles esc and ctrl+c in a login box.
func (s *sessionView) dialogEsc() tea.Cmd {
	d := s.dlg
	switch d.phase {
	case phaseStarting:
		d.cancelAfterStart = true
		return nil
	case phaseRemoving:
		return nil
	case phaseFailed:
		if d.removed {
			s.appendBlock(NewCancelMarker("Sign-in to " + d.opt.ProviderID + " cancelled: another client removed " + d.opt.ProviderID + "."))
		}
		s.closeDialog()
		return nil
	case phaseLost, phaseConfirm:
		s.closeDialog()
		return nil
	}
	if d.loginID == "" {
		s.closeDialog()
		return nil
	}
	return s.cancelOpenLogin(d)
}

// cancelOpenLogin closes the box and asks the leader to cancel the login. The
// note comes from the answers, because a key that was saved already stays saved.
func (s *sessionView) cancelOpenLogin(d *loginDialog) tea.Cmd {
	rec := &closedLogin{loginID: d.loginID, provider: d.opt.ProviderID, isKey: d.isKey}
	s.closed[d.loginID] = rec
	s.closeDialog()
	c, id := s.client, d.loginID
	return func() tea.Msg {
		ctx, cancel := callContext()
		defer cancel()
		cancelled, err := c.CancelLogin(ctx, id)
		return cancelAnsweredMsg{loginID: id, cancelled: cancelled, err: err}
	}
}

// copyCmd copies the URL or the device code of the box. It never copies what
// the user typed.
func (s *sessionView) copyCmd() tea.Cmd {
	d := s.dlg
	if d == nil || d.isKey {
		return nil
	}
	text := d.url
	if d.kind == protocol.ACPLoginKindDevice {
		text = d.userCode
	}
	if text == "" {
		return nil
	}
	s.copiedUntil = s.now().Add(copiedFor)
	return tea.SetClipboard(text)
}

// ---- secrets ----

// sendSecret sends the key, the code or the address of the box once.
func (s *sessionView) sendSecret() tea.Cmd {
	d := s.dlg
	if d.secret.empty() {
		return nil
	}
	if s.conn != connUp {
		s.reconnect.hintUntil = s.now().Add(noticeFor)
		return nil
	}
	d.req++
	d.phase, d.since = phaseChecking, s.now()
	d.refusedText, d.refusedNext = "", ""
	dlg, req, c, opt := d.id, d.req, s.client, d.opt
	secret := d.secret.take()
	if d.isKey {
		d.loginID = newLoginID()
		id := d.loginID
		return func() tea.Msg {
			ctx, cancel := context.WithTimeout(context.Background(), keySaveTimeout)
			defer cancel()
			out, err := c.SaveAPIKey(ctx, id, opt.ProviderID, secret)
			return keySavedMsg{dlg: dlg, req: req, loginID: id, out: out, err: err}
		}
	}
	id := d.loginID
	return func() tea.Msg {
		ctx, cancel := callContext()
		defer cancel()
		err := c.SendLoginCode(ctx, id, secret)
		return codeSentMsg{dlg: dlg, req: req, loginID: id, err: err}
	}
}

func (s *sessionView) onCodeSent(msg codeSentMsg) {
	d := s.dlg
	if d == nil || d.id != msg.dlg || d.req != msg.req || d.loginID != msg.loginID || d.phase != phaseChecking {
		return
	}
	if msg.err == nil {
		return // the answer of the sign-in comes from the wait call
	}
	s.failDialog(d, "Sign-in failed.", loginNothingSaved, true)
}

// ---- answers ----

func (s *sessionView) onLoginStarted(msg loginStartedMsg) tea.Cmd {
	d := s.dlg
	if d == nil || d.id != msg.dlg || d.req != msg.req || d.phase != phaseStarting {
		// The box is gone. A sign-in that started anyway is cancelled.
		if msg.err == nil && msg.res.LoginID != "" {
			return s.silentCancelCmd(msg.res.LoginID)
		}
		return nil
	}
	if msg.err != nil {
		if d.cancelAfterStart {
			s.closeDialog()
			return nil
		}
		return s.onStartFailed(d, msg.err)
	}
	d.loginID = msg.res.LoginID
	d.kind = msg.res.Kind
	d.url = sanitizeLine(msg.res.URL)
	d.userCode = sanitizeLine(msg.res.UserCode)
	if t, err := time.Parse(time.RFC3339, msg.res.ExpiresAt); err == nil {
		d.expires = t
	}
	d.since = s.now()
	if d.cancelAfterStart {
		return s.cancelOpenLogin(d)
	}
	d.phase = phaseWaiting
	if d.hasInput() {
		d.phase = phaseInput
	}
	return s.waitLoginCmd(d)
}

// sanitizeLine keeps text from the leader on one row without control bytes.
func sanitizeLine(text string) string { return strings.TrimSpace(oneLine(text)) }

func (s *sessionView) onStartFailed(d *loginDialog, err error) tea.Cmd {
	var re *RequestError
	switch {
	case errors.Is(err, ErrNeedsLeader):
		s.closeDialog()
		s.setNotice(needsLeaderText, false)
	case errors.As(err, &re) && re.Kind == string(protocol.ACPErrProviderBusy):
		s.failDialog(d, "Could not start: another client is signing in to "+d.opt.ProviderID+".", loginTryAgainText, true)
	default:
		reason := strings.TrimRight(errText(err), ". ")
		s.failDialog(d, "Could not start: "+reason+".", loginTryAgainText, true)
	}
	return nil
}

// failDialog shows a failed box.
func (s *sessionView) failDialog(d *loginDialog, text, next string, retry bool) {
	d.secret.reset()
	d.phase = phaseFailed
	d.failedText, d.failedNext, d.retryable = text, next, retry
}

func (s *sessionView) waitLoginCmd(d *loginDialog) tea.Cmd {
	c, id := s.client, d.loginID
	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), loginWaitMax)
		defer cancel()
		out, err := c.WaitLogin(ctx, id)
		return loginEndedMsg{loginID: id, out: out, err: err}
	}
}

func (s *sessionView) onKeySaved(msg keySavedMsg) tea.Cmd {
	d := s.dlg
	if d == nil || d.id != msg.dlg || d.req != msg.req || d.loginID != msg.loginID {
		return s.onClosedOutcome(msg.loginID, msg.out, msg.err)
	}
	if d.phase != phaseChecking {
		return nil
	}
	if msg.err != nil {
		return s.onOutcomeError(d, msg.err)
	}
	return s.onOutcome(d, msg.out)
}

func (s *sessionView) onLoginEnded(msg loginEndedMsg) tea.Cmd {
	d := s.dlg
	if d == nil || d.loginID != msg.loginID || d.loginID == "" {
		return s.onClosedOutcome(msg.loginID, msg.out, msg.err)
	}
	if !d.loginOpen() {
		return nil
	}
	if msg.err != nil {
		return s.onOutcomeError(d, msg.err)
	}
	return s.onOutcome(d, msg.out)
}

func (s *sessionView) onOutcomeError(d *loginDialog, err error) tea.Cmd {
	switch {
	case errors.Is(err, ErrOutcomeUnknown), errors.Is(err, ErrDisconnected):
		s.lostDialog(d)
	case errors.Is(err, ErrNeedsLeader):
		s.closeDialog()
		s.setNotice(needsLeaderText, false)
	default:
		s.failDialog(d, "Sign-in failed.", loginNothingSaved, !d.isKey)
		if d.isKey {
			s.refuseKey(d, "Could not check the key with "+d.opt.ProviderID+". Nothing was saved.")
		}
	}
	return nil
}

// lostDialog shows that the leader is gone. The sign-in is not resumed.
func (s *sessionView) lostDialog(d *loginDialog) {
	d.secret.reset()
	d.phase = phaseLost
	d.failedText = "The sign-in stopped because the leader is gone."
	d.failedNext = "Nothing was saved. When the leader is back, start again with /providers."
	d.retryable = false
}

// refuseKey shows the reason a key was not saved and unlocks the input.
func (s *sessionView) refuseKey(d *loginDialog, text string) {
	d.secret.reset()
	d.phase = phaseInput
	d.refusedText, d.refusedNext = text, keyRefusedNextText
}

// onOutcome applies the end of a sign-in or key save to an open box.
func (s *sessionView) onOutcome(d *loginDialog, out protocol.ACPLoginOutcome) tea.Cmd {
	provider := d.opt.ProviderID
	switch out.Status {
	case protocol.ACPLoginStatusSignedIn, protocol.ACPLoginStatusSaved:
		opt, isKey := d.opt, d.isKey
		s.closeDialog()
		return s.announceSaved(opt, out, isKey)
	case protocol.ACPLoginStatusRefused:
		s.refuseKey(d, keyRefusalText(provider, out))
	case protocol.ACPLoginStatusExpired:
		s.failDialog(d, deviceExpiredText, deviceExpiredNext, true)
	case protocol.ACPLoginStatusCancelled:
		if out.Reason == protocol.ACPLoginReasonRemoved {
			d.removed = true
			s.failDialog(d, "Sign-in cancelled: another client removed "+provider+".", "Nothing was saved. You can sign in again.", true)
			break
		}
		s.closeDialog()
	default:
		if d.isKey {
			s.refuseKey(d, "Could not check the key with "+provider+". Nothing was saved.")
			break
		}
		text := "Sign-in failed."
		if out.Reason == protocol.ACPLoginReasonAccessDenied {
			text = "Sign-in failed: the browser returned access_denied."
		}
		s.failDialog(d, text, loginNothingSaved, true)
	}
	return s.flushDeferred()
}

// keyRefusalText words a refused key. It holds only the fixed fields of the
// outcome.
func keyRefusalText(provider string, out protocol.ACPLoginOutcome) string {
	text := provider + " refused the key"
	if out.ProviderCode != "" {
		text += ": " + oneLine(out.ProviderCode)
	}
	if out.HTTPStatus > 0 {
		text += " (HTTP " + strconv.Itoa(out.HTTPStatus) + ")"
	}
	return text + ". Nothing was saved."
}

// ---- cancel answers and closed logins ----

func (s *sessionView) silentCancelCmd(loginID string) tea.Cmd {
	c := s.client
	return func() tea.Msg {
		ctx, cancel := callContext()
		defer cancel()
		_, _ = c.CancelLogin(ctx, loginID)
		return nil
	}
}

func (s *sessionView) onCancelAnswered(msg cancelAnsweredMsg) tea.Cmd {
	rec := s.closed[msg.loginID]
	if rec == nil {
		return nil
	}
	rec.cancelAnswered, rec.cancelled, rec.cancelErr = true, msg.cancelled, msg.err
	return s.resolveClosed(rec)
}

func (s *sessionView) onClosedOutcome(loginID string, out protocol.ACPLoginOutcome, err error) tea.Cmd {
	rec := s.closed[loginID]
	if rec == nil {
		return nil // an answer for a login that no box owns
	}
	switch {
	case err != nil:
		// The link broke before the end was known.
		delete(s.closed, loginID)
		s.appendBlock(NewCancelMarker("Sign-in to " + rec.provider + " stopped because the leader is gone."))
		return nil
	case out.Status == protocol.ACPLoginStatusSignedIn, out.Status == protocol.ACPLoginStatusSaved:
		rec.outcome = &out
	default:
		delete(s.closed, loginID)
		s.appendBlock(NewCancelMarker("Sign-in to " + rec.provider + " cancelled. Nothing was saved."))
		return nil
	}
	return s.resolveClosed(rec)
}

// resolveClosed writes the note of a closed login when both answers are in.
func (s *sessionView) resolveClosed(rec *closedLogin) tea.Cmd {
	if !rec.cancelAnswered {
		return nil
	}
	switch {
	case rec.cancelled:
		delete(s.closed, rec.loginID)
		s.appendBlock(NewCancelMarker("Sign-in to " + rec.provider + " cancelled. Nothing was saved."))
	case rec.outcome != nil:
		delete(s.closed, rec.loginID)
		opt := protocol.ACPLoginOption{ProviderID: rec.provider}
		return s.announceSaved(opt, *rec.outcome, rec.isKey)
	case rec.cancelErr != nil:
		delete(s.closed, rec.loginID)
		s.appendBlock(NewCancelMarker("Sign-in to " + rec.provider + " stopped because the leader is gone."))
	}
	return nil
}

// ---- success ----

// announceSaved draws the note and the rail line of a saved credential. The
// number of models comes from the model list fetched after the outcome.
func (s *sessionView) announceSaved(opt protocol.ACPLoginOption, out protocol.ACPLoginOutcome, isKey bool) tea.Cmd {
	s.noteOwnGeneration(out.Generation)
	provider := opt.ProviderID
	return s.fetchModelsCmd(func(res *protocol.ACPModelsResult) tea.Cmd {
		n, listed := 0, res != nil
		if listed {
			seen := map[string]bool{}
			for _, m := range res.Models {
				if m.Provider == provider && !seen[m.ID] {
					seen[m.ID] = true
					n++
				}
			}
		}
		noModel := s.noModel()
		counts := ""
		if listed {
			counts = plural(n, "model")
		}
		var first string
		switch {
		case isKey && out.NotChecked:
			first = provider + ": Saved. This provider cannot be checked before use."
		case isKey:
			first = provider + ": API key saved."
		case out.Account != "":
			first = provider + ": signed in as " + oneLine(out.Account) + "."
		default:
			first = provider + ": signed in."
		}
		if listed && (!isKey || !out.NotChecked) {
			first += " " + counts + " ready."
		}
		var more []string
		if noModel {
			more = append(more, "Choose a model with /model.")
		}
		s.appendBlock(NewSuccessNote(first, more...))

		subject := provider + " signed in"
		if isKey {
			subject = provider + " key saved"
		}
		rail := subject
		switch {
		case !listed:
			rail += " · model list not refreshed"
		default:
			rail += " · " + counts
		}
		if noModel && listed {
			rail += " · choose one with /model"
		}
		s.setResult(rail)
		return s.flushDeferred()
	})
}

func plural(n int, word string) string {
	if n == 1 {
		return "1 " + word
	}
	return strconv.Itoa(n) + " " + word + "s"
}

// fetchModelsCmd reads the model list again. done runs with the list, or with
// nil when the list could not be read.
func (s *sessionView) fetchModelsCmd(done func(res *protocol.ACPModelsResult) tea.Cmd) tea.Cmd {
	s.fetchSeq++
	token := s.fetchSeq
	if done != nil {
		s.afterModels[token] = done
	}
	c, gen := s.client, s.gen
	return func() tea.Msg {
		ctx, cancel := callContext()
		defer cancel()
		res, err := c.Models(ctx)
		return modelsFetchedMsg{gen: gen, token: token, res: res, err: err}
	}
}

func (s *sessionView) onModelsFetched(msg modelsFetchedMsg) tea.Cmd {
	done := s.afterModels[msg.token]
	delete(s.afterModels, msg.token)
	if msg.gen != s.gen {
		return nil
	}
	var res *protocol.ACPModelsResult
	if msg.err == nil {
		res = &msg.res
		if s.applyModelList(msg.token, msg.res) {
			s.refreshOpenModelSelector(msg.res)
		}
	}
	if s.appliedFetch == s.fetchSeq {
		res = &s.modelList
	}
	if done != nil {
		return done(res)
	}
	return nil
}

func (s *sessionView) applyModelList(token uint64, res protocol.ACPModelsResult) bool {
	if token < s.fetchSeq || token < s.appliedFetch {
		return false
	}
	s.appliedFetch, s.modelList, s.models = token, res, res.Models
	return true
}

// ---- removal ----

func (s *sessionView) removeCmd() tea.Cmd {
	d := s.dlg
	if s.conn != connUp {
		s.reconnect.hintUntil = s.now().Add(noticeFor)
		return nil
	}
	d.req++
	d.phase = phaseRemoving
	dlg, req, c, opt := d.id, d.req, s.client, d.opt
	return func() tea.Msg {
		ctx, cancel := callContext()
		defer cancel()
		res, err := c.Logout(ctx, opt.ProviderID, opt.MethodID)
		return logoutDoneMsg{dlg: dlg, req: req, res: res, err: err}
	}
}

func (s *sessionView) onLogoutDone(msg logoutDoneMsg) tea.Cmd {
	d := s.dlg
	if d == nil || d.id != msg.dlg || d.req != msg.req || d.phase != phaseRemoving {
		return nil
	}
	provider := d.opt.ProviderID
	if msg.err != nil {
		switch {
		case errors.Is(msg.err, ErrOutcomeUnknown), errors.Is(msg.err, ErrDisconnected):
			s.failDialog(d, "Could not tell if the credential was removed: the leader is gone.", "", false)
		default:
			s.failDialog(d, "Could not remove the credential: "+strings.TrimRight(errText(msg.err), ". ")+".", "", false)
		}
		return nil
	}
	s.closeDialog()
	s.noteOwnGeneration(msg.res.Generation)
	if !msg.res.Removed {
		s.setNotice("Nothing to remove: "+provider+" has no saved credential.", false)
		return tea.Batch(s.flushDeferred(), s.fetchModelsCmd(nil))
	}
	rows := []string{}
	if msg.res.EnvVar != "" {
		rows = append(rows, msg.res.EnvVar+" is still set, so "+provider+" stays usable.")
	}
	if msg.res.ClearedModel != "" {
		s.hadModel = true
		name := s.refOf(msg.res.ClearedModel).id
		if name == "" {
			name = msg.res.ClearedModel
		}
		rows = append(rows, "The current model "+name+" is no longer available. Choose one with /model.")
		if !msg.res.ClearPending {
			s.modelID = ""
			s.modelSeq++
			s.lastRef = modelRef{}
		}
	}
	s.appendBlock(NewSuccessNote("Removed the saved credential for "+provider+".", rows...))
	return tea.Batch(s.flushDeferred(), s.fetchModelsCmd(nil))
}

// ---- changes by other clients ----

// noteOwnGeneration records the providers generation of a change that this
// client made. The broadcast of that change shows nothing.
func (s *sessionView) noteOwnGeneration(g uint64) {
	if g > s.ownGen {
		s.ownGen = g
	}
}

// inflightFor tells that this client has a sign-in or removal open for a
// provider, so a change notice for it may be this client's own.
func (s *sessionView) inflightFor(provider string) bool {
	if d := s.dlg; d != nil && d.opt.ProviderID == provider {
		switch d.phase {
		case phaseStarting, phaseInput, phaseChecking, phaseWaiting, phaseRemoving:
			return true
		}
	}
	for _, rec := range s.closed {
		if rec.provider == provider {
			return true
		}
	}
	return false
}

// onProvidersChanged reacts to the leader's announcement of a change.
func (s *sessionView) onProvidersChanged(msg ProvidersChangedMsg) tea.Cmd {
	if msg.Generation > s.providersGen {
		s.providersGen = msg.Generation
	}
	var cmds []tea.Cmd
	if s.pbox != nil {
		s.pbox.gen = max(s.pbox.gen, msg.Generation)
		cmds = append(cmds, s.listProvidersCmd())
	}
	switch {
	case msg.Generation <= s.ownGen:
		cmds = append(cmds, s.fetchModelsCmd(nil))
	case s.inflightFor(msg.Provider):
		s.deferred = append(s.deferred, msg)
		cmds = append(cmds, s.fetchModelsCmd(nil))
	default:
		cmds = append(cmds, s.showChanged(msg))
	}
	return tea.Batch(cmds...)
}

// flushDeferred shows the changes that waited for the end of this client's own
// change and were not part of it. It returns the commands that fetch the model
// lists for their rail lines.
func (s *sessionView) flushDeferred() tea.Cmd {
	if len(s.deferred) == 0 {
		return nil
	}
	waiting := s.deferred
	s.deferred = nil
	var cmds []tea.Cmd
	for _, msg := range waiting {
		if msg.Generation > s.ownGen {
			cmds = append(cmds, s.showChanged(msg))
		}
	}
	return tea.Batch(cmds...)
}

// showChanged fetches the model list and then names the change on the rail.
func (s *sessionView) showChanged(msg ProvidersChangedMsg) tea.Cmd {
	provider, change := msg.Provider, msg.Change
	return s.fetchModelsCmd(func(res *protocol.ACPModelsResult) tea.Cmd {
		text := "Providers changed in the leader: " + provider
		switch {
		case change == protocol.ACPProviderChangeRemoved:
			text += " removed"
		case res == nil:
			text += " credential saved · model list not refreshed"
		default:
			seen := map[string]bool{}
			n := 0
			for _, m := range res.Models {
				if m.Provider == provider && !seen[m.ID] {
					seen[m.ID] = true
					n++
				}
			}
			text += " credential saved · " + plural(n, "model")
		}
		s.setNotice(text, false)
		return nil
	})
}

// ---- link state ----

// onBoxDisconnected stops a sign-in whose link is gone. The box says so and the
// sign-in is not resumed after the reconnect.
func (s *sessionView) onBoxDisconnected() {
	d := s.dlg
	if d == nil {
		return
	}
	switch d.phase {
	case phaseStarting, phaseInput, phaseChecking, phaseWaiting:
		if d.isKey && d.phase == phaseInput && d.loginID == "" {
			return // nothing was sent yet; the typed key waits for the link
		}
		s.lostDialog(d)
	case phaseRemoving:
		s.failDialog(d, "Could not tell if the credential was removed: the leader is gone.", "", false)
	}
}

// onBoxReconnected loads the list again after the link is back.
func (s *sessionView) onBoxReconnected() tea.Cmd {
	if s.pbox != nil {
		return s.listProvidersCmd()
	}
	return nil
}

// ---- stop ----

// stopCmd cancels the open login before the program ends on a signal. The
// buffers are dropped first.
func (s *sessionView) stopCmd() tea.Cmd {
	d := s.dlg
	s.dropSecrets()
	if d == nil || !d.loginOpen() || d.loginID == "" || s.conn != connUp {
		return nil
	}
	c, id := s.client, d.loginID
	s.dlg = nil
	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), stopCancelTimeout)
		defer cancel()
		_, _ = c.CancelLogin(ctx, id)
		return nil
	}
}

// ---- rail ----

// waitRow is the rail line of a login that waits, or "".
func (s *sessionView) waitRow() string {
	d := s.dlg
	if d == nil {
		return ""
	}
	short := s.m.width < shortWidth
	elapsed := " " + s.elapsed(d.since)
	cancel := " · esc to cancel"
	if short {
		cancel = ""
	}
	var word string
	switch d.phase {
	case phaseStarting:
		word = "Starting the sign-in…"
		if short {
			word = "Starting…"
		}
		return s.statusRow(RoleAccent, GlyphWorking, word, elapsed+cancel)
	case phaseChecking:
		if d.isKey && uncheckedProviders[d.opt.ProviderID] {
			return ""
		}
		switch {
		case d.isKey:
			word = "Checking the key with " + d.opt.ProviderID + "…"
		case d.kind == protocol.ACPLoginKindBrowserAddress:
			word = "Checking the address with " + d.opt.ProviderID + "…"
		default:
			word = "Checking the code with " + d.opt.ProviderID + "…"
		}
		if short {
			word = "Checking…"
		}
		return s.statusRow(RoleAccent, GlyphWorking, word, elapsed+cancel)
	case phaseWaiting, phaseInput:
		if d.isKey || d.loginID == "" {
			return ""
		}
		tail := elapsed + cancel
		switch d.kind {
		case protocol.ACPLoginKindBrowserAddress:
			word, tail = "Waiting for the browser or an address…", elapsed
		case protocol.ACPLoginKindCopyCode:
			word, tail = "Waiting for a code…", elapsed
		case protocol.ACPLoginKindDevice:
			word = "Waiting for approval…"
		default:
			word = "Waiting for the browser…"
		}
		if short {
			word, tail = "Waiting…", elapsed
		}
		return s.statusRow(RoleAccent, GlyphWorking, word, tail)
	}
	return ""
}

// dialogTicks tells that the open box shows a time that changes.
func (s *sessionView) dialogTicks() bool {
	d := s.dlg
	if d == nil {
		return false
	}
	switch d.phase {
	case phaseStarting, phaseChecking, phaseWaiting:
		return true
	case phaseInput:
		return d.loginID != "" && !d.isKey
	}
	return false
}
