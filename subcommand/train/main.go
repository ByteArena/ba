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

func TrainAction(
	tps int,
	host string,
	vizport int,
	nobrowser bool,
	recordFile string,
	agentimages []string,
	isDebug bool,
	mapName string,
	shouldProfile,
	dumpRaw bool,
	durationSeconds int,
) (bool, error) {

	if shouldProfile {
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

	if durationSeconds > 0 {
		d := time.Duration(durationSeconds) * time.Second
		gameDuration = &d
	}

	shutdownChan := make(chan bool)
	debug := func(str string) {}

	if isDebug {
		debug = func(str string) {
			fmt.Printf(DebugColor("[debug] %s\n"), str)
		}
	}

	if host == "" {
		ip, err := utils.GetCurrentIP()
		utils.Check(err, "Could not determine host IP; you can specify using the `--host` flag.")
		host = ip
	}

	if len(agentimages) == 0 {
		return SHOW_USAGE, bettererrors.New("No agents were specified")
	}

	runPreflightChecks()

	// Make message broker client
	brokerclient, err := NewMemoryMessageClient()
	utils.Check(err, "ERROR: Could not connect to messagebroker")

	mappack, errMappack := mappack.UnzipAndGetHandles(mapcmd.GetMapLocation(mapName))
	if errMappack != nil {
		utils.FailWith(errMappack)
	}

	gamedescription, err := NewMockGame(tps, mappack)
	if err != nil {
		utils.FailWith(err)
	}

	game := deathmatch.NewDeathmatchGame(gamedescription)

	orchestrator := container.MakeLocalContainerOrchestrator(host)

	arenaServerUUID := ""

	srv := arenaserver.NewServer(
		host,
		orchestrator,
		gamedescription,
		game,
		arenaServerUUID,
		brokerclient,
		gameDuration,
	)

	for _, dockerImageName := range agentimages {
		agentManifest, err := types.GetAgentManifestByDockerImageName(dockerImageName, orchestrator)
		if err != nil {
			return DONT_SHOW_USAGE, err
		}

		agent := &types.Agent{Manifest: agentManifest}

		gamedescription.AddAgent(agent)
		srv.RegisterAgent(agent)
	}

	// consume server events
	go func() {
		events := srv.Events()

		for {
			msg := <-events

			switch t := msg.(type) {
			case arenaserver.EventStatusGameUpdate:
				fmt.Printf(GameColor("[game] %s\n"), t.Status)

			case arenaserver.EventAgentLog:
				fmt.Printf(AgentColor("[agent] %s\n"), t.Value)

			case arenaserver.EventLog:
				fmt.Printf(LogColor("[log] %s\n"), t.Value)

			case arenaserver.EventDebug:
				debug(t.Value)

			case arenaserver.EventError:
				utils.FailWith(t.Err)

			case arenaserver.EventWarn:
				utils.WarnWith(t.Err)

			case arenaserver.EventHeadsUp:
				fmt.Printf(HeadsUpColor("[headsup] %s\n"), t.Value)

			case arenaserver.EventRawComm:
				if dumpRaw {
					fmt.Printf(AgentColor("[agent] %s\n"), t.Value)
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
	if recordFile != "" {
		recorder = recording.MakeSingleArenaRecorder(recordFile)
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
		"127.0.0.1:"+strconv.Itoa(vizport),
		mapName,
		func() ([]*viztypes.VizGame, error) { return vizgames, nil },
		recorder,
		mappack,
	)

	vizservice.Start()

	serverShutdown, startErr := srv.Start()

	if startErr != nil {
		utils.FailWith(startErr)
	}

	url := "http://localhost:" + strconv.Itoa(vizport) + "/arena/1"

	if !nobrowser {
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
