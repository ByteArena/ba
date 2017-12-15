package train

import (
	"encoding/json"
	"time"

	"github.com/bytearena/core/common/agentmanifest"
	"github.com/bytearena/core/common/mappack"
	"github.com/bytearena/core/common/types"
	"github.com/bytearena/core/common/types/mapcontainer"

	bettererrors "github.com/xtuc/better-errors"
)

type MockGame struct {
	tps          int
	agents       []types.Agent
	mapContainer *mapcontainer.MapContainer
}

func NewMockGame(tps int, mapbundle *mappack.MappackInMemoryArchive) (*MockGame, error) {

	jsonsource, err := mapbundle.Open("map.json")
	if err != nil {
		return nil, bettererrors.
			New("Could not find map.json inside the map bundle").
			With(bettererrors.NewFromErr(err))
	}

	var mapContainer mapcontainer.MapContainer
	if err := json.Unmarshal(jsonsource, &mapContainer); err != nil {

		return nil, bettererrors.
			New("map.json exists inside the map bundle, but is not valid.").
			With(bettererrors.NewFromErr(err))
	}

	return &MockGame{
		tps:          tps,
		agents:       make([]types.Agent, 0),
		mapContainer: &mapContainer,
	}, nil
}

func (game *MockGame) GetId() string {
	return "1"
}

func (game *MockGame) GetName() string {
	return "Trainer game"
}

func (game *MockGame) GetTps() int {
	return game.tps
}

func (game *MockGame) GetRunStatus() int {
	return 1 // TODO: use enum GameRunStatus.Running
}

func (game *MockGame) GetLaunchedAt() string {
	return time.Now().Format("2006-01-02T15:04:05-0700")
}

func (game *MockGame) GetEndedAt() string {
	return ""
}

func (game *MockGame) AddAgent(agentmanifest agentmanifest.AgentManifest) {

	game.agents = append(game.agents, types.Agent{
		Manifest: agentmanifest,
	})
}

func (game *MockGame) GetAgents() []types.Agent {
	return game.agents
}

func (game *MockGame) GetMapContainer() *mapcontainer.MapContainer {
	return game.mapContainer
}
