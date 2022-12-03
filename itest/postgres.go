package itest

import (
	"context"
	"encoding/binary"
	"errors"
	"fmt"
	"io"
	"log"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"testing"
	"time"

	"github.com/breez/lntest"
	"github.com/docker/docker/api/types"
	"github.com/docker/docker/api/types/container"
	"github.com/docker/docker/client"
	"github.com/docker/go-connections/nat"
	"github.com/jackc/pgx/v4/pgxpool"
)

type PostgresContainer struct {
	id       string
	password string
	port     uint32
	cli      *client.Client
}

func StartPostgresContainer(t *testing.T, ctx context.Context, logfile string) *PostgresContainer {
	cli, err := client.NewClientWithOpts(client.FromEnv)
	lntest.CheckError(t, err)

	image := "postgres:15"
	_, _, err = cli.ImageInspectWithRaw(ctx, image)
	if err != nil {
		if !client.IsErrNotFound(err) {
			lntest.CheckError(t, err)
		}

		pullReader, err := cli.ImagePull(ctx, image, types.ImagePullOptions{})
		lntest.CheckError(t, err)
		_, err = io.Copy(io.Discard, pullReader)
		pullReader.Close()
		lntest.CheckError(t, err)
	}

	port, err := lntest.GetPort()
	lntest.CheckError(t, err)
	createResp, err := cli.ContainerCreate(ctx, &container.Config{
		Image: image,
		Cmd: []string{
			"postgres",
			"-c",
			"log_statement=all",
		},
		Env: []string{
			"POSTGRES_DB=postgres",
			"POSTGRES_PASSWORD=pgpassword",
			"POSTGRES_USER=postgres",
		},
		Healthcheck: &container.HealthConfig{
			Test:     []string{"CMD-SHELL", "pg_isready -U postgres"},
			Interval: time.Second,
			Timeout:  time.Second,
			Retries:  10,
		},
	}, &container.HostConfig{
		PortBindings: nat.PortMap{
			"5432/tcp": []nat.PortBinding{
				{HostPort: strconv.FormatUint(uint64(port), 10)},
			},
		},
	},
		nil,
		nil,
		"",
	)
	lntest.CheckError(t, err)

	err = cli.ContainerStart(ctx, createResp.ID, types.ContainerStartOptions{})
	lntest.CheckError(t, err)

	ct := &PostgresContainer{
		id:       createResp.ID,
		password: "pgpassword",
		port:     port,
		cli:      cli,
	}

HealthCheck:
	for {
		inspect, err := cli.ContainerInspect(ctx, createResp.ID)
		lntest.CheckError(t, err)

		status := inspect.State.Health.Status
		switch status {
		case "unhealthy":
			lntest.CheckError(t, errors.New("container unhealthy"))
		case "healthy":
			for {
				pgxPool, err := pgxpool.Connect(context.Background(), ct.ConnectionString())
				if err == nil {
					pgxPool.Close()
					break HealthCheck
				}

				time.Sleep(50 * time.Millisecond)
			}
		default:
			time.Sleep(200 * time.Millisecond)
		}
	}

	go ct.monitorLogs(logfile)
	return ct
}

func (c *PostgresContainer) monitorLogs(logfile string) {
	i, err := c.cli.ContainerLogs(context.Background(), c.id, types.ContainerLogsOptions{
		ShowStderr: true,
		ShowStdout: true,
		Timestamps: false,
		Follow:     true,
		Tail:       "40",
	})
	if err != nil {
		log.Printf("Could not get container logs: %v", err)
		return
	}
	defer i.Close()

	file, err := os.OpenFile(logfile, os.O_WRONLY|os.O_CREATE|os.O_APPEND, 0600)
	if err != nil {
		log.Printf("Could not create container log file: %v", err)
		return
	}
	defer file.Close()

	hdr := make([]byte, 8)
	for {
		_, err := i.Read(hdr)
		if err != nil {
			return
		}
		count := binary.BigEndian.Uint32(hdr[4:])
		dat := make([]byte, count)
		_, err = i.Read(dat)
		if err != nil {
			return
		}
		_, err = file.Write(dat)
		if err != nil {
			return
		}
	}
}

func (c *PostgresContainer) ConnectionString() string {
	return fmt.Sprintf("postgres://postgres:%s@127.0.0.1:%d/postgres", c.password, c.port)
}

func (c *PostgresContainer) Shutdown(ctx context.Context) error {
	defer c.cli.Close()
	timeout := time.Second
	err := c.cli.ContainerStop(ctx, c.id, &timeout)
	return err
}

func (c *PostgresContainer) Cleanup(ctx context.Context) error {
	cli, err := client.NewClientWithOpts(client.FromEnv)
	if err != nil {
		return err
	}
	defer cli.Close()
	return cli.ContainerRemove(ctx, c.id, types.ContainerRemoveOptions{
		Force: true,
	})
}

func (c *PostgresContainer) RunMigrations(t *testing.T, ctx context.Context, migrationDir string) {
	filenames, err := filepath.Glob(filepath.Join(migrationDir, "*.up.sql"))
	lntest.CheckError(t, err)

	sort.Strings(filenames)

	pgxPool, err := pgxpool.Connect(context.Background(), c.ConnectionString())
	lntest.CheckError(t, err)
	defer pgxPool.Close()

	for _, filename := range filenames {
		data, err := os.ReadFile(filename)
		lntest.CheckError(t, err)

		_, err = pgxPool.Exec(ctx, string(data))
		lntest.CheckError(t, err)
	}
}
