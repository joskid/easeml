package scheduler

import (
	"github.com/ds3lab/easeml/process"
	"github.com/ds3lab/easeml/database/model"
	"github.com/ds3lab/easeml/logger"
	"github.com/ds3lab/easeml/storage"
	"github.com/ds3lab/easeml/workers"
	"fmt"
	"os"
	"path/filepath"
)

// Start is the entry point.
func Start(context process.Context) {
	fmt.Println("scheduler called")

	log := logger.NewProcessLogger()

	// Initialize the storage context.
	storageContext := storage.Context{WorkingDir: context.WorkingDir}

	// TODO: Move all this code to the server.

	modelContext, err := model.Connect(context.DatabaseAddress, context.DatabaseName, false)
	if err != nil {
		log.WriteFatal(fmt.Sprintf("fatal: %+v", err))
	}
	defer modelContext.Session.Close()

	// Register the new process.
	var process model.Process
	process, err = modelContext.StartProcess(model.ProcScheduler)
	if err != nil {
		log.WriteFatal(fmt.Sprintf("fatal: %+v", err))
	}
	defer modelContext.SetProcessStatus(process.ID, model.ProcTerminated)
	log.WithFields("process-id", process.ID.Hex(), "PID", process.ProcessID).WriteInfo("SCHEDULER PROCESS STARTED")
	log.ProcessID = process.ID.Hex()

	// Create log file.
	processPath, err := storageContext.GetProcessPath(process.ID.Hex(), "")
	if err != nil {
		panic(err)
	}
	logFilePath := filepath.Join(processPath, process.Type+".log")
	logFile, err := os.OpenFile(logFilePath, os.O_WRONLY|os.O_CREATE, storage.DefaultFilePerm)
	if err != nil {
		panic(err)
	}
	log.AddJSONWriter(logFile)
	defer logFile.Close()

	// Log the root user in and generate their API key.
	// TODO: Log out later (if no other controllers are alive).
	user, err := modelContext.UserLogin()
	if err != nil {
		log.WriteFatal(fmt.Sprintf("fatal: %+v", err))
	}
	log.WithFields("api-key", user.APIKey).WriteInfo("ROOT USER LOGGED IN")

	// Report the root API key to the API key channel.
	context.RootAPIKey <- user.APIKey

	// Run the downloader.
	workersContext := workers.Context{
		ModelContext:   modelContext,
		StorageContext: storageContext,
		ProcessID:      process.ID,
		Period:         context.ListenerPeriod,
		Logger:         log,
	}

	// Process keepalive goroutine.
	go func() {
		workersContextCopy := workersContext.Clone()
		defer workersContextCopy.ModelContext.Session.Close()
		workersContextCopy.ProcessKeepaliveWorker(context.KeepAlivePeriod)
	}()

	// Objective runner worker. This is the only goroutine so we will block.
	workersContextCopy := workersContext.Clone()
	defer workersContextCopy.ModelContext.Session.Close()
	workersContextCopy.OptimizerRunListener(context.OptimizerID)
}
