package workflow

import (
	"fmt"
	"log/slog"
	"os"
	"strconv"
	"strings"

	"github.com/aravindh-murugesan/openstack-virt-enforcer/openstack-virt-enforcer/internal/virt"
	"github.com/google/uuid"
	"github.com/lmittmann/tint"
)

// SetupLogger configures a structured logger template for uniformity across workflows.
//
// It uses the [tint] handler to provide colorized output for better terminal
// readability. The logger is automatically contextualized with the
// "cloud_profile" attribute to assist in multi-cloud log filtering.
func SetupLogger(level string, cloudName string) *slog.Logger {
	var logLevel slog.Level
	switch level {
	case "debug":
		logLevel = slog.LevelDebug
	case "warn":
		logLevel = slog.LevelWarn
	case "error":
		logLevel = slog.LevelError
	default:
		logLevel = slog.LevelInfo
	}

	handler := tint.NewHandler(os.Stderr, &tint.Options{
		Level: logLevel,
	})

	return slog.New(handler).With("cloud_profile", cloudName)
}

func (l *Logger) SetupLogger() {

	if l.Level == "" {
		l.Level = "info"
	}

	var logLevel slog.Level
	switch l.Level {
	case "debug":
		logLevel = slog.LevelDebug
	case "warn":
		logLevel = slog.LevelWarn
	case "error":
		logLevel = slog.LevelError
	default:
		logLevel = slog.LevelInfo
	}

	if l.Instance == nil {
		handler := tint.NewHandler(os.Stderr, &tint.Options{
			Level:   logLevel,
			NoColor: false,
		})
		l.Instance = slog.New(handler)
	}

	if l.RunID == "" {
		l.RunID = fmt.Sprintf("ve-%s", uuid.NewString())
	}
}

func ParseIotuneInput(iopsOverride string) (virt.IOTune, error) {

	reqIoTune := virt.IOTune{
		SizeIopsSec: 16384,
	}

	iopsOverrideP1 := strings.Split(iopsOverride, ",")
	if len(iopsOverrideP1) != 3 {
		return reqIoTune, fmt.Errorf("Requested IOTune limits are invalid. (malformed request: %s)", iopsOverride)
	}

	totalIOPSValue, terr := strconv.Atoi(iopsOverrideP1[0])
	writeIOPSValue, werr := strconv.Atoi(iopsOverrideP1[1])
	readIOPSValue, rerr := strconv.Atoi(iopsOverrideP1[2])

	if terr != nil || werr != nil || rerr != nil {
		return reqIoTune, fmt.Errorf("Unable to formulate iops limit values metadata/input (%s): %w", iopsOverride, terr)
	}

	if totalIOPSValue > 0 {
		reqIoTune.TotalIopsSec = uint64(totalIOPSValue)
	} else if writeIOPSValue > 0 && readIOPSValue > 0 {
		reqIoTune.WriteIopsSec = uint64(writeIOPSValue)
		reqIoTune.ReadIopsSec = uint64(readIOPSValue)
	} else {
		return reqIoTune, fmt.Errorf("Total IOPS, Write IOPS, Read IOPS cannot be all zero. (malformed request: %s)", iopsOverride)
	}

	return reqIoTune, nil

}
