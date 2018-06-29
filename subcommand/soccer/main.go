package soccer

import (
	"fmt"
	"log"
	"reflect"
	"strconv"
	"time"

	"github.com/ttacon/chalk"
	bettererrors "github.com/xtuc/better-errors"

	notify "github.com/bitly/go-notify"

	"github.com/bytearena/core/arenaserver"
	"github.com/bytearena/core/arenaserver/container"
	"github.com/bytearena/core/common"
	"github.com/bytearena/core/common/mq"
	"github.com/bytearena/core/common/types"
	"github.com/bytearena/core/common/utils"
	"github.com/bytearena/core/common/utils/vector"
	"github.com/bytearena/core/common/visualization"
	viztypes "github.com/bytearena/core/common/visualization/types"
	"github.com/bytearena/core/game/soccer"
)

const (
	TIME_BEFORE_FORCE_QUIT = 5 * time.Second
)

// TODO(sven): we should disable the colors when the terminal has no frontend
// and/or expliclty pass an --no-colors argument.
var (
	DebugColor   = chalk.Cyan.Color
	GameColor    = chalk.Blue.Color
	AgentColor   = chalk.Green.Color
	HeadsUpColor = chalk.Yellow.Color
	LogColor     = chalk.ResetColor.Color
)

type SoccerRunArguments struct {
	Host        string
	Vizport     int
	Vizhost     string
	Agentimages []string
	IsQuiet     bool
	IsDebug     bool
}

const (
	SHOW_USAGE      = true
	DONT_SHOW_USAGE = false
)

func Run(args SoccerRunArguments) (bool, error) {

	shutdownChan := make(chan bool)

	if args.Host == "" {
		ip, err := utils.GetCurrentIP()
		utils.Check(err, "Could not determine host IP; you can specify using the `--host` flag.")
		args.Host = ip
	}

	if len(args.Agentimages) == 0 {
		return SHOW_USAGE, bettererrors.New("No agents were specified")
	}

	// Make message broker client
	brokerclient, err := NewMemoryMessageClient()
	utils.Check(err, "ERROR: Could not connect to messagebroker")

	game := soccer.NewSoccerGame()
	gamedescription := NewSoccerGameDescription(20) // tps: 20
	gameDuration := time.Minute * 15

	orchestrator := container.MakeLocalContainerOrchestrator(args.Host)

	arenaServerUUID := ""

	srv := arenaserver.NewServer(
		args.Host,
		orchestrator,
		gamedescription, // TODO
		game,
		arenaServerUUID,
		brokerclient,
		&gameDuration,
		args.IsDebug,
	)

	// Regular agents
	for _, dockerImageName := range args.Agentimages {
		agentManifest, err := types.GetAgentManifestByDockerImageName(dockerImageName, orchestrator)
		if err != nil {
			return DONT_SHOW_USAGE, err
		}

		agent := &types.Agent{Manifest: agentManifest}

		gamedescription.AddAgent(agent)

		spawningPoint := vector.MakeVector2(0, 0)
		srv.RegisterAgent(agent, &spawningPoint)
	}

	// consume server events
	go func() {
		events := srv.Events()

		for {
			msg := <-events

			switch t := msg.(type) {
			case arenaserver.EventStatusGameUpdate:
				if !args.IsQuiet {
					fmt.Printf(GameColor("[game] %s\n"), t.Status)
				}

			case arenaserver.EventAgentLog:
				fmt.Printf(AgentColor("[agent] %s\n"), t.Value)

			case arenaserver.EventLog:
				if !args.IsQuiet {
					fmt.Printf(LogColor("[log] %s\n"), t.Value)
				}

			case arenaserver.EventDebug:
				log.Println(t.Value)

			case arenaserver.EventError:
				utils.FailWith(t.Err)

			case arenaserver.EventWarn:
				utils.WarnWith(t.Err)

			case arenaserver.EventHeadsUp:
				fmt.Printf(HeadsUpColor("[headsup] %s\n"), t.Value)

			case arenaserver.EventRawComm:
				if args.IsDebug {
					fmt.Printf(DebugColor("[debug from: %s] %s\n"), t.From, t.Value)
				}

			case arenaserver.EventClose:
				return

			default:
				msg := fmt.Sprintf("Unsupported message of type %s", reflect.TypeOf(msg))
				panic(msg)
			}
		}
	}()

	go func() {
		utils.LogFn = func(service, message string) {
			fmt.Println(message)
		}
	}()

	// handling signals
	go func() {
		<-common.SignalHandler()
		shutdownChan <- true
	}()

	go common.StreamState(srv, brokerclient, "trainer")

	brokerclient.Subscribe("viz", "message", func(msg mq.BrokerMessage) {
		gameID := gamedescription.GetId()
		notify.PostTimeout("viz:message:"+gameID, string(msg.Data), time.Millisecond)
	})

	// TODO(jerome): refac webclient path / serving

	vizgames := make([]*viztypes.VizGame, 1)
	vizgames[0] = viztypes.NewVizGame(game, gamedescription)

	vizservice := visualization.NewVizService(
		args.Vizhost+":"+strconv.Itoa(args.Vizport),
		"no-map",
		func() ([]*viztypes.VizGame, error) { return vizgames, nil },
		nil,
		nil,
	)

	vizservice.Start()

	serverShutdown, startErr := srv.Start()

	if startErr != nil {
		utils.FailWith(startErr)
	}

	srv.Log(arenaserver.EventHeadsUp{"Game running"})

	// Wait until someone asks for shutdown
	select {
	case <-serverShutdown:
	case <-shutdownChan:
	}

	// Force quit if the programs didn't exit
	go func() {
		<-time.After(TIME_BEFORE_FORCE_QUIT)

		berror := bettererrors.New("Forced shutdown")

		utils.FailWith(berror)
	}()

	log.Println("Shutdown...")

	srv.Stop()

	vizservice.Stop()

	return DONT_SHOW_USAGE, nil
}
