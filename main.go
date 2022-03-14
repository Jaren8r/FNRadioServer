package main

import (
	"context"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"errors"
	"flag"
	"net/http"
	"os"
	"os/exec"
	"regexp"
	"strings"
	"sync"
	"time"

	"github.com/jackc/pgx/v4/pgxpool"

	"github.com/joho/godotenv"

	"github.com/gin-gonic/gin"
)

type FNRadioServer struct {
	Debug          bool
	Router         *gin.Engine
	DB             *pgxpool.Pool
	StreamStations StreamStationStore
	Parties        PartyStore
}

func (server *FNRadioServer) getStation(c *gin.Context) {
	authenticatedUser := c.MustGet("user").(User)
	if c.Param("user") != authenticatedUser.ID {
		party := server.Parties.GetUserParty(authenticatedUser.ID)
		if !(party != nil && party.Members[0] == c.Param("user")) {
			c.JSON(403, gin.H{
				"error": "you do not have permission to get this station",
			})

			return
		}
	}

	station := Station{}

	err := server.DB.QueryRow(context.TODO(), "SELECT type, source FROM stations WHERE user_id = $1 AND id = $2", c.Param("user"), c.Param("station")).Scan(&station.Type, &station.Source)
	if err != nil {
		c.JSON(500, gin.H{"error": err.Error()})
		return
	}

	blurl, err := server.createBlurl(&station, c)
	if err != nil {
		c.JSON(500, gin.H{
			"error": err.Error(),
		})

		return
	}

	_, _ = c.Writer.Write(blurl)
}

const (
	InvalidAuthorizationHeaderError = "Invalid authorization header"
)

func (server *FNRadioServer) handleAuth(c *gin.Context) {
	authorization := strings.Split(c.GetHeader("Authorization"), " ")

	if !strings.EqualFold(authorization[0], "basic") || len(authorization) != 2 {
		c.AbortWithStatusJSON(401, gin.H{
			"error": InvalidAuthorizationHeaderError,
		})

		return
	}

	decoded, err := base64.StdEncoding.DecodeString(authorization[1])
	if err != nil {
		c.AbortWithStatusJSON(401, gin.H{
			"error": InvalidAuthorizationHeaderError,
		})

		return
	}

	credentials := strings.Split(string(decoded), ":")

	if len(credentials) != 2 {
		c.AbortWithStatusJSON(401, gin.H{
			"error": InvalidAuthorizationHeaderError,
		})

		return
	}

	user := User{}

	err = server.DB.QueryRow(context.TODO(), "SELECT * FROM users WHERE id = $1 AND secret = $2", credentials[0], credentials[1]).Scan(&user.ID, &user.Secret)
	if err != nil {
		c.AbortWithStatusJSON(401, gin.H{
			"error": err.Error(),
		})

		return
	}

	c.Set("user", user)
	c.Next()
}

func (server *FNRadioServer) createUser(c *gin.Context) {
	id := generateID()
	secret := generateID()

	_, err := server.DB.Query(context.TODO(), "INSERT INTO users (id, secret) VALUES ($1, $2)", id, secret)
	if err != nil {
		c.JSON(500, gin.H{
			"error": err.Error(),
		})

		return
	}

	c.JSON(200, gin.H{
		"id":     id,
		"secret": secret,
	})
}

func (server *FNRadioServer) getCurrentUser(c *gin.Context) {
	currentUser := c.MustGet("user").(User)

	stations, err := server.getUserStations(currentUser.ID)

	if err != nil {
		c.JSON(500, gin.H{
			"error": err.Error(),
		})

		return
	}

	bindings, err := server.getUserBindings(currentUser.ID)

	if err != nil {
		c.JSON(500, gin.H{
			"error": err.Error(),
		})

		return
	}

	stationsMap := make(map[string]Station)
	bindingsMap := make(map[string]Binding)

	for _, station := range stations {
		stationsMap[station.ID] = station
	}

	for _, binding := range bindings {
		bindingsMap[binding.ID] = binding
	}

	c.JSON(200, gin.H{
		"stations": stationsMap,
		"bindings": bindingsMap,
	})
}

func (server *FNRadioServer) getPartyLeader(c *gin.Context, userToGet string) {
	bindings, err := server.getUserBindings(userToGet)

	if err != nil {
		c.JSON(500, gin.H{
			"error": err.Error(),
		})

		return
	}

	bindingsMap := make(map[string]Binding)

	for _, binding := range bindings {
		bindingsMap[binding.ID] = binding
	}

	c.JSON(200, gin.H{
		"bindings": bindingsMap,
	})
}

func (server *FNRadioServer) getUser(c *gin.Context) {
	currentUser := c.MustGet("user").(User)

	userToGet := c.Param("user")

	if userToGet == "@me" || userToGet == currentUser.ID {
		server.getCurrentUser(c)
		return
	}

	party := server.Parties.GetUserParty(currentUser.ID)
	if party != nil && party.Members[0] == userToGet {
		server.getPartyLeader(c, userToGet)
		return
	}

	c.JSON(403, gin.H{
		"error": "you do not have permission to get this user",
	})
}

type createStationPayload struct {
	Type   string `json:"type"`
	Source string `json:"source"`
}

type bindStationPayload struct {
	StationUser string `json:"station_user"`
	StationID   string `json:"station_id"`
}

func (server *FNRadioServer) handleYouTubeSource(id string) ([]string, error) {
	folder := "YT_" + id

	if _, err := os.Stat("media/" + folder); os.IsNotExist(err) {
		err := server.startYouTubeDownload(id)
		if err != nil {
			return nil, err
		}
	}

	return []string{folder}, nil
}

func (server *FNRadioServer) getSourceStreams(source string) ([]string, error) {
	ytID, _ := extractYouTubeID(source)

	if ytID != "" {
		return server.handleYouTubeSource(ytID)
	}

	ytPlaylist, _ := extractYouTubePlaylistID(source)
	if ytPlaylist != "" {
		return server.handleYouTubePlaylist(ytPlaylist)
	}

	return nil, errors.New("invalid source")
}

func (server *FNRadioServer) getSourceStream(source string) (string, error) {
	folders, err := server.getSourceStreams(source)
	if err != nil {
		return "", err
	}

	switch len(folders) {
	case 0:
		return "", errors.New("no sources found")
	case 1:
		return folders[0], nil
	default:
		return server.getPlaylistStream(folders)
	}
}

func (server *FNRadioServer) nukeSource(folder string) {
	_ = os.RemoveAll("media/" + folder)

	rows, err := server.DB.Query(context.Background(), "SELECT user_id, id FROM stations WHERE source = $1", folder)
	if err != nil {
		return
	}

	for rows.Next() {
		var stationUser, stationID string

		err := rows.Scan(&stationUser, &stationID)
		if err != nil {
			continue
		}

		_, _ = server.DB.Exec(context.Background(), "DELETE FROM bindings WHERE station_user = $1 AND station_id = $2", stationUser, stationID)
	}

	_, _ = server.DB.Exec(context.Background(), "SELECT FROM stations WHERE source = $1", folder)
}

func (server *FNRadioServer) createPlaylistStream(folder string, sources []string) {
OUTER:
	for {
		for _, source := range sources {
			if _, err := os.Stat("media/" + source); os.IsNotExist(err) {
				server.nukeSource(folder)
				return
			}

			if _, err := os.Stat("media/" + source + "/master.m3u8"); os.IsNotExist(err) {
				time.Sleep(time.Second)
				continue OUTER
			}
		}

		break
	}

	playlistEntries := make([]string, 0)
	for _, source := range sources {
		playlistEntries = append(playlistEntries, "file '../"+source+"/master.m3u8'")
	}

	err := os.WriteFile("media/"+folder+"/playlist.txt", []byte(strings.Join(playlistEntries, "\n")), 0644)
	if err != nil {
		server.nukeSource(folder)
		return
	}

	command := exec.Command("ffmpeg", "-f", "concat", "-safe", "0", "-i", "media/"+folder+"/playlist.txt", "-hls_playlist_type", "vod", "-hls_time", "2", "-hls_segment_type", "fmp4", "-hls_flags", "discont_start", "-c:a", "copy", "-master_pl_name", "master.m3u8", "media/"+folder+"/output.m3u8")

	err = command.Run()
	if err != nil {
		server.nukeSource(folder)
		return
	}
}

func (server *FNRadioServer) getPlaylistStream(sources []string) (string, error) {
	hash := sha256.Sum256([]byte(strings.Join(sources, "\n")))
	folder := "PL_" + hex.EncodeToString(hash[:16])

	if _, err := os.Stat("media/" + folder); os.IsNotExist(err) {
		err = os.Mkdir("media/"+folder, 0755)
		if err != nil {
			return "", err
		}

		go server.createPlaylistStream(folder, sources)
	}

	return folder, nil
}

func (server *FNRadioServer) createStation(c *gin.Context) { // nolint:funlen
	var payload createStationPayload

	err := c.BindJSON(&payload)
	if err != nil {
		c.JSON(500, gin.H{
			"error": err.Error(),
		})
	}

	user := c.MustGet("user").(User)
	existing := server.getUserStation(user.ID, c.Param("station"))

	if existing != nil {
		if payload.Type == StationTypeStatic && existing.Type == StationTypeStatic {
			stream, err := server.getSourceStream(payload.Source)
			if err != nil {
				c.JSON(400, gin.H{
					"error": err.Error(),
				})

				return
			}

			_, err = server.DB.Exec(context.TODO(), "UPDATE stations SET source = $1 WHERE user_id = $2 AND id = $3", stream, user.ID, c.Param("station"))
			if err != nil {
				c.JSON(500, gin.H{
					"error": err.Error(),
				})

				return
			}

			c.Status(204)

			return
		}

		c.JSON(409, gin.H{
			"error": "station already exists",
		})

		return
	}

	var source string

	switch payload.Type {
	case StationTypeStatic:
		source, err = server.getSourceStream(payload.Source)
		if err != nil {
			c.JSON(400, gin.H{
				"error": err.Error(),
			})

			return
		}
	case StationTypeStream:
	// NOOP:
	default:
		c.JSON(400, gin.H{
			"error": "invalid station type",
		})

		return
	}

	_, err = server.DB.Exec(context.TODO(), "INSERT INTO stations (user_id, id, type, source) VALUES ($1, $2, $3, $4)", user.ID, c.Param("station"), payload.Type, source)
	if err != nil {
		c.JSON(500, gin.H{
			"error": err.Error(),
		})

		return
	}

	c.Status(204)
}

func (server *FNRadioServer) deleteStation(c *gin.Context) {
	user := c.MustGet("user").(User)

	station := server.getUserStation(user.ID, c.Param("station"))
	if station == nil {
		c.JSON(404, gin.H{
			"error": "station not found",
		})

		return
	}

	_, err := server.DB.Exec(context.TODO(), "DELETE FROM stations WHERE user_id = $1 AND id = $2", user.ID, c.Param("station"))
	if err != nil {
		c.JSON(500, gin.H{
			"error": err.Error(),
		})

		return
	}

	_, _ = server.DB.Exec(context.TODO(), "DELETE FROM bindings WHERE station_user = $1 AND station_id = $2", user.ID, c.Param("station"))

	if station.Type == StationTypeStream {
		streamStation := server.StreamStations.Get(station)
		if streamStation != nil {
			streamStation.Quit <- struct{}{}
			server.StreamStations.Remove(streamStation)
		}
	}

	c.Status(204)
}

type addToQueuePayload struct {
	Source string `json:"source"`
}

func (server *FNRadioServer) addToQueue(c *gin.Context) {
	var payload addToQueuePayload

	err := c.BindJSON(&payload)
	if err != nil {
		c.JSON(500, gin.H{
			"error": err.Error(),
		})
	}

	user := c.MustGet("user").(User)

	station := server.getUserStation(user.ID, c.Param("station"))
	if station == nil {
		c.JSON(404, gin.H{
			"error": "station not found",
		})

		return
	}

	if station.Type != StationTypeStream {
		c.JSON(400, gin.H{
			"error": "station type must be " + StationTypeStream,
		})

		return
	}

	sources, err := server.getSourceStreams(payload.Source)
	if err != nil {
		c.JSON(400, gin.H{
			"error": err.Error(),
		})

		return
	}

	streamStation := server.StreamStations.GetOrCreate(station)

	for _, source := range sources {
		streamStation.Queue.Add(&StreamQueueElement{
			source: source,
			data:   make([]byte, 0),
			mu:     sync.Mutex{},
		})
	}

	c.Status(204)
}

func (server *FNRadioServer) createBinding(c *gin.Context) {
	var payload bindStationPayload

	err := c.BindJSON(&payload)
	if err != nil {
		c.JSON(500, gin.H{
			"error": err.Error(),
		})
	}

	user := c.MustGet("user").(User)

	if payload.StationUser != user.ID {
		c.JSON(403, gin.H{
			"error": "station must belong to the requesting user",
		})

		return
	}

	station := server.getUserStation(payload.StationUser, payload.StationID)
	if station == nil {
		c.JSON(404, gin.H{
			"error": "station not found",
		})

		return
	}

	_, _ = server.DB.Exec(context.TODO(), "DELETE FROM bindings WHERE user_id = $1 AND id = $2", user.ID, c.Param("binding"))

	_, err = server.DB.Exec(context.TODO(), "INSERT INTO bindings (user_id, id, station_user, station_id) VALUES ($1, $2, $3, $4)", user.ID, c.Param("binding"), payload.StationUser, payload.StationID)
	if err != nil {
		c.JSON(500, gin.H{
			"error": err.Error(),
		})

		return
	}

	c.Status(204)
}

func (server *FNRadioServer) deleteBinding(c *gin.Context) {
	user := c.MustGet("user").(User)

	binding := server.getUserBinding(user.ID, c.Param("binding"))
	if binding == nil {
		c.JSON(404, gin.H{
			"error": "binding not found",
		})

		return
	}

	_, err := server.DB.Exec(context.TODO(), "DELETE FROM bindings WHERE user_id = $1 AND id = $2", user.ID, c.Param("binding"))
	if err != nil {
		c.JSON(500, gin.H{
			"error": err.Error(),
		})

		return
	}

	c.Status(204)
}

var streamRegex = regexp.MustCompile(`^/media/(STR_[0-9a-f]{32})/`)

func (server *FNRadioServer) handleMedia(c *gin.Context) {
	match := streamRegex.FindStringSubmatch(c.Request.URL.Path)
	if len(match) > 0 {
		streamStation := server.StreamStations.GetByFolder(match[1])
		if streamStation != nil {
			streamStation.LastRequest = time.Now()
		}
	}
}

func (server *FNRadioServer) setParty(c *gin.Context) {
	var clientParty ClientParty

	err := c.BindJSON(&clientParty)
	if err != nil {
		c.JSON(400, gin.H{
			"error": err.Error(),
		})
	}

	user := c.MustGet("user").(User)

	server.Parties.RemoveUser(user.ID)

	if clientParty.Match != "" {
		if !clientParty.Validate() {
			c.JSON(400, gin.H{
				"error": err.Error(),
			})

			return
		}

		party, err := server.Parties.CreateOrJoinParty(user.ID, clientParty)
		if err != nil {
			c.JSON(400, gin.H{
				"error": err.Error(),
			})

			return
		}

		c.JSON(200, gin.H{
			"leader": party.Members[0],
		})
	}

	c.Status(204)
}

func (server *FNRadioServer) setupRouter() {
	if server.Debug {
		gin.SetMode(gin.DebugMode)
	} else {
		gin.SetMode(gin.ReleaseMode)
	}

	server.Router = gin.New()

	if server.Debug {
		server.Router.Use(gin.Logger())
	}

	if _, err := os.Stat("media"); os.IsNotExist(err) {
		err = os.Mkdir("media", 0755)
		if err != nil {
			panic(err)
		}
	}

	server.Router.Use(server.handleMedia)

	server.Router.Static("/media", "media")

	server.Router.POST("/users", server.createUser)

	server.Router.GET("/users/:user", server.handleAuth, server.getUser)

	server.Router.GET("/users/:user/stations/:station", server.handleAuth, server.getStation)

	server.Router.PUT("/users/@me/stations/:station", server.handleAuth, server.createStation)

	server.Router.DELETE("/users/@me/stations/:station", server.handleAuth, server.deleteStation)

	server.Router.PUT("/users/@me/stations/:station/queue", server.handleAuth, server.addToQueue)

	server.Router.PUT("/users/@me/bindings/:binding", server.handleAuth, server.createBinding)

	server.Router.DELETE("/users/@me/bindings/:binding", server.handleAuth, server.deleteBinding)

	server.Router.POST("/users/@me/party", server.handleAuth, server.setParty)
}

func (server *FNRadioServer) Destroy() {
	server.cleanupStreamStations()
}

func main() {
	_ = godotenv.Load()

	debugPtr := flag.Bool("debug", false, "")

	flag.Parse()

	server := FNRadioServer{
		Debug: *debugPtr,
	}

	server.cleanupBrokenStations()

	server.cleanupStreamStations()

	server.setupRouter()

	server.setupDB()

	err := http.ListenAndServe(os.Getenv("LISTEN_ADDRESS"), server.Router)
	if err != nil {
		panic(err)
	}
}
