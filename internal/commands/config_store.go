package commands

import "echopoint-cli/internal/config"

// loadStore reads the config file the run selected with --config or
// ECHOPOINT_CONFIG, or the user's own when neither was given, so that a command
// writes the file a command reads.
func (state *AppState) loadStore() (config.Store, error) {
	if state.ConfigPath == "" {
		store, _, err := config.LoadStore()
		return store, err
	}
	store, _, err := config.LoadStoreFrom(state.ConfigPath)
	return store, err
}

// saveStore writes the store back to the file loadStore read.
func (state *AppState) saveStore(store config.Store) (string, error) {
	if state.ConfigPath == "" {
		return config.SaveStore(store)
	}
	return config.SaveStoreTo(state.ConfigPath, store)
}
