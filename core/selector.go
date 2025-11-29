package core

import (
	"errors"
	"fmt"
	"strings"
	"sync"

	"github.com/InazumaV/V2bX/api/panel"
	"github.com/InazumaV/V2bX/conf"
)

type Selector struct {
	cores map[string]Core
	nodes sync.Map
}

func NewSelector(c []conf.CoreConfig) (Core, error) {
	cs := make(map[string]Core, len(c))
	for _, t := range c {
		f, ok := cores[strings.ToLower(t.Type)]
		if !ok {
			return nil, fmt.Errorf("unknown core type: %s", t.Type)
		}
		core1, err := f(&t)
		if err != nil {
			return nil, fmt.Errorf("failed to initialize %s core: %w", t.Type, err)
		}
		name := t.Type
		if t.Name != "" {
			name = t.Name
		}
		cs[name] = core1
	}
	return &Selector{
		cores: cs,
	}, nil
}

func (s *Selector) Start() error {
	for name, core := range s.cores {
		err := core.Start()
		if err != nil {
			return fmt.Errorf("failed to start %s core: %w", name, err)
		}
	}
	return nil
}

func (s *Selector) Close() error {
	var errs []error
	for name, core := range s.cores {
		if err := core.Close(); err != nil {
			errs = append(errs, fmt.Errorf("failed to close %s core: %w", name, err))
		}
	}
	return errors.Join(errs...)
}

func isSupported(protocol string, protocols []string) bool {
	for _, p := range protocols {
		if protocol == p {
			return true
		}
	}
	return false
}

func (s *Selector) AddNode(tag string, info *panel.NodeInfo, option *conf.Options) error {
	var core Core

	// Select core based on configuration
	if option.CoreName != "" {
		// use name to select core
		c, ok := s.cores[option.CoreName]
		if !ok {
			return fmt.Errorf("specified core name '%s' not found", option.CoreName)
		}
		core = c
	} else {
		// use type to select core
		for _, c := range s.cores {
			// If no specific core is required or core type matches
			if option.Core == "" || option.Core == c.Type() {
				// Check if protocol is supported
				if isSupported(info.Type, c.Protocols()) {
					core = c
					break
				}
			}
		}
	}

	if core == nil {
		return errors.New("no suitable core found for the node type")
	}

	// Process core-specific options
	if option.Core == "" {
		option.Core = core.Type()
		err := option.UnmarshalJSON(option.RawOptions)
		if err != nil {
			return fmt.Errorf("unmarshal option error: %w", err)
		}
		option.RawOptions = nil
	}

	err := core.AddNode(tag, info, option)
	if err != nil {
		return err
	}
	s.nodes.Store(tag, core)
	return nil
}

func (s *Selector) DelNode(tag string) error {
	t, ok := s.nodes.Load(tag)
	if !ok {
		return errors.New("node not found")
	}

	err := t.(Core).DelNode(tag)
	if err != nil {
		return err
	}
	s.nodes.Delete(tag)
	return nil
}

func (s *Selector) AddUsers(p *AddUsersParams) (added int, err error) {
	t, ok := s.nodes.Load(p.Tag)
	if !ok {
		return 0, errors.New("node not found")
	}
	return t.(Core).AddUsers(p)
}

func (s *Selector) GetUserTrafficSlice(tag string, reset bool) ([]panel.UserTraffic, error) {
	t, ok := s.nodes.Load(tag)
	if !ok {
		return nil, errors.New("node not found")
	}
	return t.(Core).GetUserTrafficSlice(tag, reset)
}

func (s *Selector) DelUsers(users []panel.UserInfo, tag string, info *panel.NodeInfo) error {
	t, ok := s.nodes.Load(tag)
	if !ok {
		return errors.New("node not found")
	}
	return t.(Core).DelUsers(users, tag, info)
}

func (s *Selector) Protocols() []string {
	protocols := make([]string, 0)
	seen := make(map[string]bool) // Prevent duplicates

	for _, core := range s.cores {
		for _, p := range core.Protocols() {
			if !seen[p] {
				protocols = append(protocols, p)
				seen[p] = true
			}
		}
	}
	return protocols
}

func (s *Selector) Type() string {
	t := "Selector("
	names := make([]string, 0, len(s.cores))

	for name := range s.cores {
		names = append(names, name)
	}

	for i, name := range names {
		if i > 0 {
			t += " "
		}
		t += name
	}
	t += ")"
	return t
}
