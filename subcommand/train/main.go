package train

import (
	"fmt"
	"log"
	"os"
	"reflect"
	"runtime/pprof"
	"strconv"
	"time"

	"github.com/ttacon/chalk"

	bettererrors "github.com/xtuc/better-errors"

	notify "github.com/bitly/go-notify"

	"github.com/skratchdot/open-golang/open"

	"github.com/bytearena/core/arenaserver"
	"github.com/bytearena/core/arenaserver/container"
	"github.com/bytearena/core/common"
	"github.com/bytearena/core/common/mappack"
	"github.com/bytearena/core/common/mq"
	"github.com/bytearena/core/common/recording"
	"github.com/bytearena/core/common/types"
	"github.com/bytearena/core/common/utils"
	"github.com/bytearena/core/common/visualization"
	viztypes "github.com/bytearena/core/common/visualization/types"
	"github.com/bytearena/core/game/deathmatch"

	"github.com/bytearena/ba/subcommand/build"
	mapcmd "github.com/bytearena/ba/subcommand/map"
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

const (
	SHOW_USAGE      = true
	DONT_SHOW_USAGE = false
)

const (
	TIME_BEFORE_FORCE_QUIT = 5 * time.Second
)

type TrainActionArguments struct {
	Tps                int
	Host               string
	Vizport            int
	Vizhost            string
	Nobrowser          bool
	RecordFile         string
	Agentimages        []string
	WatchedAgentimages []string
	IsDebug            bool
	IsQuiet            bool
	MapName            string
	ShouldProfile      bool
	DurationSeconds    int
}

func stringInStringSlice(l string, list []string) bool {
	for _, r := range list {
		if l == r {
			return true
		}
	}
	return false
}

func TrainAction(args TrainActionArguments) (bool, error) {

	if args.ShouldProfile {
		f, err := os.Create("./cpu.prof")
		if err != nil {
			log.Fatal("could not create CPU profile: ", err)
		}
		if err := pprof.StartCPUProfile(f); err != nil {
			log.Fatal("could not start CPU profile: ", err)
		}
		defer pprof.StopCPUProfile()
	}

	var gameDuration *time.Duration

	if args.DurationSeconds > 0 {
		d := time.Duration(args.DurationSeconds) * time.Second
		gameDuration = &d
	}

	shutdownChan := make(chan bool)
	debug := func(str string) {}

	if args.IsDebug {
		debug = func(str string) {
			fmt.Printf(DebugColor("[debug] %s\n"), str)
		}
	}

	if args.Host == "" {
		ip, err := utils.GetCurrentIP()
		utils.Check(err, "Could not determine host IP; you can specify using the `--host` flag.")
		args.Host = ip
	}

	if len(args.Agentimages) == 0 && len(args.WatchedAgentimages) == 0 {
		return SHOW_USAGE, bettererrors.New("No agents were specified")
	}

	runPreflightChecks()

	// Make message broker client
	brokerclient, err := NewMemoryMessageClient()
	utils.Check(err, "ERROR: Could not connect to messagebroker")

	mappack, errMappack := mappack.UnzipAndGetHandles(mapcmd.GetMapLocation(args.MapName))
	if errMappack != nil {
		utils.FailWith(errMappack)
	}

	gamedescription, err := NewMockGame(args.Tps, mappack)
	if err != nil {
		utils.FailWith(err)
	}

	game := deathmatch.NewDeathmatchGame(gamedescription)

	orchestrator := container.MakeLocalContainerOrchestrator(args.Host)

	arenaServerUUID := ""

	srv := arenaserver.NewServer(
		args.Host,
		orchestrator,
		gamedescription,
		game,
		arenaServerUUID,
		brokerclient,
		gameDuration,
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
		srv.RegisterAgent(agent, nil)
	}

	// Watched agents
	for _, agentPath := range args.WatchedAgentimages {

		go func() {
			args := build.Arguments{
				WatchMode: true,
			}

			_, buildErr := build.Main(agentPath, args)

			if buildErr != nil {
				berror := bettererrors.
					New("Failed to build agent").
					With(buildErr)

				utils.FailWith(berror)
			}
		}()

		<-time.After(3 * time.Second)

		// Get image name from agent manifest file
		agentManifest, parseManifestError := types.ParseAgentManifestFromDir(agentPath)

		if parseManifestError != nil {
			return DONT_SHOW_USAGE, bettererrors.
				New("Could not parse manifest").
				With(parseManifestError)
		}

		dockerImageName := agentManifest.Id

		agentManifest, err := types.GetAgentManifestByDockerImageName(dockerImageName, orchestrator)
		if err != nil {
			return DONT_SHOW_USAGE, err
		}

		agent := &types.Agent{Manifest: agentManifest}

		gamedescription.AddAgent(agent)
		srv.RegisterAgent(agent, nil)

		go func() {
			for {
				// Fake changes in dir for now
				<-time.After(5 * time.Second)

				err := srv.ReloadAgent(agent)

				if err != nil {
					berror := bettererrors.
						New("Could not reload agent").
						With(err)

					utils.FailWith(berror)
					return
				}
			}
		}()
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
				debug(t.Value)

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

	var recorder recording.RecorderInterface = recording.MakeEmptyRecorder()
	if args.RecordFile != "" {
		recorder = recording.MakeSingleArenaRecorder(args.RecordFile)
	}

	recorder.RecordMetadata(gamedescription.GetId(), gamedescription.GetMapContainer())

	brokerclient.Subscribe("viz", "message", func(msg mq.BrokerMessage) {
		gameID := gamedescription.GetId()

		recorder.Record(gameID, string(msg.Data))
		notify.PostTimeout("viz:message:"+gameID, string(msg.Data), time.Millisecond)
	})

	// TODO(jerome): refac webclient path / serving

	vizgames := make([]*viztypes.VizGame, 1)
	vizgames[0] = viztypes.NewVizGame(game, gamedescription)

	vizservice := visualization.NewVizService(
		args.Vizhost+":"+strconv.Itoa(args.Vizport),
		args.MapName,
		func() ([]*viztypes.VizGame, error) { return vizgames, nil },
		recorder,
		mappack,
	)

	vizservice.Start()

	serverShutdown, startErr := srv.Start()

	if startErr != nil {
		utils.FailWith(startErr)
	}

	url := "http://" + args.Vizhost + ":" + strconv.Itoa(args.Vizport) + "/arena/1"

	if !args.Nobrowser {
		open.Run(url)
	}

	srv.Log(arenaserver.EventHeadsUp{"Game running at " + url})

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

	debug("Shutdown...")

	srv.Stop()

	recorder.Close(gamedescription.GetId())
	recorder.Stop()

	vizservice.Stop()

	return DONT_SHOW_USAGE, nil
}
