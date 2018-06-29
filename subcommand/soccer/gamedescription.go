package soccer

import (
	"time"

	"github.com/bytearena/core/common/types"
	"github.com/bytearena/core/common/types/mapcontainer"
)

type SoccerGameDescription struct {
	tps    int
	agents []*types.Agent
}

func NewSoccerGameDescription(tps int) *SoccerGameDescription {
	return &SoccerGameDescription{
		tps:    tps,
		agents: make([]*types.Agent, 0),
	}
}

func (game *SoccerGameDescription) GetId() string {
	return "1"
}

func (game *SoccerGameDescription) GetName() string {
	return "Soccer game"
}

func (game *SoccerGameDescription) GetTps() int {
	return game.tps
}

func (game *SoccerGameDescription) GetRunStatus() int {
	return 1 // TODO: use enum GameRunStatus.Running
}

func (game *SoccerGameDescription) GetLaunchedAt() string {
	return time.Now().Format("2006-01-02T15:04:05-0700")
}

func (game *SoccerGameDescription) GetEndedAt() string {
	return ""
}

func (game *SoccerGameDescription) AddAgent(agent *types.Agent) {
	game.agents = append(game.agents, agent)
}

func (game *SoccerGameDescription) GetAgents() []*types.Agent {
	return game.agents
}

func (game *SoccerGameDescription) GetMapContainer() *mapcontainer.MapContainer {
	return nil
}
