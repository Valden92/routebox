package api

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"time"

	"github.com/Valden92/routebox/internal/config"
	"github.com/Valden92/routebox/internal/singbox"
	"github.com/Valden92/routebox/internal/subscription"
)

// Protected by Server.autoMu. This descriptor belongs to one successful core
// startup. Rule updates cannot change its transport or call stop/start/recover.
type liveRouterRules struct {
	dir, configPath string
	core            []byte
	baseKey         [32]byte
	personal, work  bool
}

func liveBaseKey(st config.Settings) [32]byte {
	st.DomainRules, st.AppRules = nil, nil
	return routingSettingsKey(st)
}

func prepareLiveRouterRules(path string, node *subscription.Node, st config.Settings, snapshot singbox.RouterNetworkSnapshot) (*liveRouterRules, error) {
	dir, err := os.MkdirTemp(filepath.Dir(path), ".router-rule-sets-*")
	if err != nil {
		return nil, err
	}
	keep := false
	defer func() {
		if !keep {
			_ = os.RemoveAll(dir)
		}
	}()
	personal := st.PersonalVPN.Enabled && node != nil
	files, err := singbox.LiveRuleFiles(st, personal, snapshot.SystemVPNUp)
	if err != nil {
		return nil, err
	}
	for name, body := range files {
		if writeErr := writePrivateConfig(filepath.Join(dir, name), body); writeErr != nil {
			return nil, writeErr
		}
	}
	if writeErr := singbox.WriteLiveRouterConfig(path, node, st, "tun100", snapshot, dir); writeErr != nil {
		return nil, writeErr
	}
	body, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	keep = true
	return &liveRouterRules{dir: dir, configPath: st.SingBoxConfigPath, core: body, baseKey: liveBaseKey(st), personal: personal, work: snapshot.SystemVPNUp}, nil
}

// validate receives an isolated candidate referencing only staged rule files.
// Publication is atomic per file; sing-box watches these files asynchronously.
// It exposes no acknowledgement for the new contents, so this returns a
// published version, never claims an observed/confirmed runtime rule version.
func (l *liveRouterRules) publish(ctx context.Context, st config.Settings, validate func(context.Context, string) error) (string, bool, error) {
	if contextErr := ctx.Err(); contextErr != nil {
		return "", false, contextErr
	}
	if l.baseKey != liveBaseKey(st) {
		return "", false, errors.New("правила сохранены; параметры VPN изменились, обновление без перезапуска невозможно")
	}
	current, err := os.ReadFile(l.configPath)
	if err != nil || !bytes.Equal(current, l.core) {
		return "", false, errors.New("рабочая конфигурация изменилась; автоматический перезапуск запрещён")
	}
	files, err := singbox.LiveRuleFiles(st, l.personal, l.work)
	if err != nil {
		return "", false, err
	}
	names := make([]string, 0, len(files))
	for name := range files {
		names = append(names, name)
	}
	slices.Sort(names)
	hash := sha256.New()
	previous := map[string][]byte{}
	changed := false
	for _, name := range names {
		// Include filenames and explicit separators, not ambiguous concatenation.
		_, _ = hash.Write([]byte(name + "\x00"))
		_, _ = hash.Write(files[name])
		body, readErr := os.ReadFile(filepath.Join(l.dir, name))
		if readErr != nil {
			return "", false, fmt.Errorf("чтение набора правил: %w", readErr)
		}
		previous[name] = body
		changed = changed || !bytes.Equal(body, files[name])
	}
	version := hex.EncodeToString(hash.Sum(nil))
	if !changed {
		return version, false, nil
	}
	stage, err := os.MkdirTemp(filepath.Dir(l.dir), ".router-rule-check-*")
	if err != nil {
		return "", false, err
	}
	defer func() { _ = os.RemoveAll(stage) }()
	for _, name := range names {
		if writeErr := writePrivateConfig(filepath.Join(stage, name), files[name]); writeErr != nil {
			return "", false, writeErr
		}
	}
	var cfg map[string]any
	if decodeErr := json.Unmarshal(l.core, &cfg); decodeErr != nil {
		return "", false, decodeErr
	}
	for _, raw := range cfg["route"].(map[string]any)["rule_set"].([]any) {
		set := raw.(map[string]any)
		set["path"] = filepath.Join(stage, filepath.Base(set["path"].(string)))
	}
	candidate := filepath.Join(stage, "sing-box.json")
	body, err := json.Marshal(cfg)
	if err != nil {
		return "", false, err
	}
	if writeErr := writePrivateConfig(candidate, body); writeErr != nil {
		return "", false, writeErr
	}
	if checkErr := validate(ctx, candidate); checkErr != nil {
		return "", false, fmt.Errorf("проверка правил: %w", checkErr)
	}
	if contextErr := ctx.Err(); contextErr != nil {
		return "", false, contextErr
	}
	current, err = os.ReadFile(l.configPath)
	if err != nil || !bytes.Equal(current, l.core) {
		return "", false, errors.New("рабочая конфигурация изменилась во время проверки правил")
	}
	var published []string
	for _, name := range names {
		if bytes.Equal(previous[name], files[name]) {
			continue
		}
		if renameErr := os.Rename(filepath.Join(stage, name), filepath.Join(l.dir, name)); renameErr != nil {
			failures := []error{renameErr}
			for _, done := range published {
				failures = append(failures, writePrivateConfig(filepath.Join(l.dir, done), previous[done]))
			}
			return "", false, fmt.Errorf("публикация правил: %w", errors.Join(failures...))
		}
		published = append(published, name)
	}
	return version, true, nil
}

func (s *Server) publishLiveRules(ctx context.Context, reason string) (bool, error) {
	s.autoMu.Lock()
	defer s.autoMu.Unlock()
	if s.stopRequested.Load() {
		return false, errors.New("демон завершает работу")
	}
	if err := ctx.Err(); err != nil {
		return false, err
	}
	if !s.SingBox.Running() {
		return false, nil
	}
	if s.liveRules == nil {
		return false, errors.New("правила сохранены; работающий процесс не поддерживает обновление правил без перезапуска")
	}
	st := s.Store.Get()
	version, changed, err := s.liveRules.publish(ctx, st, func(checkCtx context.Context, path string) error {
		if err := singbox.ValidateConfigContext(checkCtx, st.SingBoxPath, path); err != nil {
			return err
		}
		return singbox.WaitClashAPI(checkCtx, singbox.ClashAPIAddr)
	})
	s.configApply.recordRulePublication(reason, version, changed, err)
	return changed, err
}

func (t *configApplyTracker) recordRulePublication(reason, version string, changed bool, err error) {
	t.mu.Lock()
	defer t.mu.Unlock()
	now := time.Now()
	t.state.Attempt++
	t.state.Reason, t.state.StartedAt, t.state.CompletedAt = reason, &now, &now
	t.state.Rollback, t.state.Error = "", ""
	t.state.Unchanged = false
	if err != nil {
		t.state.Phase = "rejected"
		t.state.Error = "Обновление правил не завершилось. Работающий VPN не перезапускался."
		return
	}
	t.state.PublishedRulesVersion = version
	if changed {
		t.state.Phase = "rules_published"
		// The file watcher has no acknowledgement API. Do not label publication as
		// a confirmed startup, update LastSuccessfulAt, or claim settings are live.
		t.settingsKnown = false
	} else {
		t.state.Unchanged = true
	}
}
