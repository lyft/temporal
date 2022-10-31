// The MIT License
//
// Copyright (c) 2020 Temporal Technologies Inc.  All rights reserved.
//
// Copyright (c) 2020 Uber Technologies, Inc.
//
// Permission is hereby granted, free of charge, to any person obtaining a copy
// of this software and associated documentation files (the "Software"), to deal
// in the Software without restriction, including without limitation the rights
// to use, copy, modify, merge, publish, distribute, sublicense, and/or sell
// copies of the Software, and to permit persons to whom the Software is
// furnished to do so, subject to the following conditions:
//
// The above copyright notice and this permission notice shall be included in
// all copies or substantial portions of the Software.
//
// THE SOFTWARE IS PROVIDED "AS IS", WITHOUT WARRANTY OF ANY KIND, EXPRESS OR
// IMPLIED, INCLUDING BUT NOT LIMITED TO THE WARRANTIES OF MERCHANTABILITY,
// FITNESS FOR A PARTICULAR PURPOSE AND NONINFRINGEMENT. IN NO EVENT SHALL THE
// AUTHORS OR COPYRIGHT HOLDERS BE LIABLE FOR ANY CLAIM, DAMAGES OR OTHER
// LIABILITY, WHETHER IN AN ACTION OF CONTRACT, TORT OR OTHERWISE, ARISING FROM,
// OUT OF OR IN CONNECTION WITH THE SOFTWARE OR THE USE OR OTHER DEALINGS IN
// THE SOFTWARE.

package worker

import (
	"context"
	"math/rand"
	"sync/atomic"
	"time"

	"go.temporal.io/api/serviceerror"

	"go.temporal.io/server/api/historyservice/v1"
	"go.temporal.io/server/client"
	"go.temporal.io/server/common"
	carchiver "go.temporal.io/server/common/archiver"
	"go.temporal.io/server/common/archiver/provider"
	"go.temporal.io/server/common/cluster"
	"go.temporal.io/server/common/config"
	"go.temporal.io/server/common/dynamicconfig"
	"go.temporal.io/server/common/log"
	"go.temporal.io/server/common/log/tag"
	"go.temporal.io/server/common/membership"
	"go.temporal.io/server/common/metrics"
	"go.temporal.io/server/common/namespace"
	"go.temporal.io/server/common/persistence"
	persistenceClient "go.temporal.io/server/common/persistence/client"
	"go.temporal.io/server/common/persistence/visibility/manager"
	esclient "go.temporal.io/server/common/persistence/visibility/store/elasticsearch/client"
	"go.temporal.io/server/common/primitives"
	"go.temporal.io/server/common/resource"
	"go.temporal.io/server/common/sdk"
	"go.temporal.io/server/service/worker/archiver"
	"go.temporal.io/server/service/worker/batcher"
	"go.temporal.io/server/service/worker/parentclosepolicy"
	"go.temporal.io/server/service/worker/replicator"
	"go.temporal.io/server/service/worker/scanner"
)

type (
	// Service represents the temporal-worker service. This service hosts all background processing needed for temporal cluster:
	// Replicator: Handles applying replication tasks generated by remote clusters.
	// Archiver: Handles archival of workflow histories.
	Service struct {
		logger                 log.Logger
		archivalMetadata       carchiver.ArchivalMetadata
		clusterMetadata        cluster.Metadata
		metricsClient          metrics.Client
		clientBean             client.Bean
		clusterMetadataManager persistence.ClusterMetadataManager
		metadataManager        persistence.MetadataManager
		hostInfo               *membership.HostInfo
		executionManager       persistence.ExecutionManager
		taskManager            persistence.TaskManager
		historyClient          historyservice.HistoryServiceClient
		namespaceRegistry      namespace.Registry
		workerServiceResolver  membership.ServiceResolver
		visibilityManager      manager.VisibilityManager

		archiverProvider provider.ArchiverProvider

		namespaceReplicationQueue persistence.NamespaceReplicationQueue

		persistenceBean persistenceClient.Bean

		membershipMonitor membership.Monitor

		metricsHandler metrics.MetricsHandler

		status           int32
		stopC            chan struct{}
		sdkClientFactory sdk.ClientFactory
		esClient         esclient.Client
		config           *Config

		workerManager             *workerManager
		perNamespaceWorkerManager *perNamespaceWorkerManager
		scanner                   *scanner.Scanner
		workerFactory             sdk.WorkerFactory
	}

	// Config contains all the service config for worker
	Config struct {
		ArchiverConfig                        *archiver.Config
		ScannerCfg                            *scanner.Config
		ParentCloseCfg                        *parentclosepolicy.Config
		ThrottledLogRPS                       dynamicconfig.IntPropertyFn
		PersistenceMaxQPS                     dynamicconfig.IntPropertyFn
		PersistenceGlobalMaxQPS               dynamicconfig.IntPropertyFn
		PersistenceNamespaceMaxQPS            dynamicconfig.IntPropertyFnWithNamespaceFilter
		EnablePersistencePriorityRateLimiting dynamicconfig.BoolPropertyFn
		EnableBatcher                         dynamicconfig.BoolPropertyFn
		BatcherRPS                            dynamicconfig.IntPropertyFnWithNamespaceFilter
		BatcherConcurrency                    dynamicconfig.IntPropertyFnWithNamespaceFilter
		EnableParentClosePolicyWorker         dynamicconfig.BoolPropertyFn
		PerNamespaceWorkerCount               dynamicconfig.IntPropertyFnWithNamespaceFilter

		StandardVisibilityPersistenceMaxReadQPS   dynamicconfig.IntPropertyFn
		StandardVisibilityPersistenceMaxWriteQPS  dynamicconfig.IntPropertyFn
		AdvancedVisibilityPersistenceMaxReadQPS   dynamicconfig.IntPropertyFn
		AdvancedVisibilityPersistenceMaxWriteQPS  dynamicconfig.IntPropertyFn
		EnableReadVisibilityFromES                dynamicconfig.BoolPropertyFnWithNamespaceFilter
		EnableReadFromSecondaryAdvancedVisibility dynamicconfig.BoolPropertyFnWithNamespaceFilter
		VisibilityDisableOrderByClause            dynamicconfig.BoolPropertyFn
	}
)

func NewService(
	logger resource.SnTaggedLogger,
	serviceConfig *Config,
	sdkClientFactory sdk.ClientFactory,
	esClient esclient.Client,
	archivalMetadata carchiver.ArchivalMetadata,
	clusterMetadata cluster.Metadata,
	metricsClient metrics.Client,
	clientBean client.Bean,
	clusterMetadataManager persistence.ClusterMetadataManager,
	namespaceRegistry namespace.Registry,
	executionManager persistence.ExecutionManager,
	archiverProvider provider.ArchiverProvider,
	persistenceBean persistenceClient.Bean,
	membershipMonitor membership.Monitor,
	namespaceReplicationQueue persistence.NamespaceReplicationQueue,
	metricsHandler metrics.MetricsHandler,
	metadataManager persistence.MetadataManager,
	taskManager persistence.TaskManager,
	historyClient historyservice.HistoryServiceClient,
	workerManager *workerManager,
	perNamespaceWorkerManager *perNamespaceWorkerManager,
	visibilityManager manager.VisibilityManager,
	workerFactory sdk.WorkerFactory,
) (*Service, error) {
	workerServiceResolver, err := membershipMonitor.GetResolver(common.WorkerServiceName)
	if err != nil {
		return nil, err
	}

	s := &Service{
		status:                    common.DaemonStatusInitialized,
		config:                    serviceConfig,
		sdkClientFactory:          sdkClientFactory,
		esClient:                  esClient,
		stopC:                     make(chan struct{}),
		logger:                    logger,
		archivalMetadata:          archivalMetadata,
		clusterMetadata:           clusterMetadata,
		metricsClient:             metricsClient,
		clientBean:                clientBean,
		clusterMetadataManager:    clusterMetadataManager,
		namespaceRegistry:         namespaceRegistry,
		executionManager:          executionManager,
		persistenceBean:           persistenceBean,
		workerServiceResolver:     workerServiceResolver,
		membershipMonitor:         membershipMonitor,
		archiverProvider:          archiverProvider,
		namespaceReplicationQueue: namespaceReplicationQueue,
		metricsHandler:            metricsHandler,
		metadataManager:           metadataManager,
		taskManager:               taskManager,
		historyClient:             historyClient,
		visibilityManager:         visibilityManager,

		workerManager:             workerManager,
		perNamespaceWorkerManager: perNamespaceWorkerManager,
		workerFactory:             workerFactory,
	}
	if err := s.initScanner(); err != nil {
		return nil, err
	}
	return s, nil
}

// NewConfig builds the new Config for worker service
func NewConfig(dc *dynamicconfig.Collection, persistenceConfig *config.Persistence, enableReadFromES bool) *Config {
	config := &Config{
		ArchiverConfig: &archiver.Config{
			MaxConcurrentActivityExecutionSize: dc.GetIntProperty(
				dynamicconfig.WorkerArchiverMaxConcurrentActivityExecutionSize,
				1000,
			),
			MaxConcurrentWorkflowTaskExecutionSize: dc.GetIntProperty(
				dynamicconfig.WorkerArchiverMaxConcurrentWorkflowTaskExecutionSize,
				1000,
			),
			MaxConcurrentActivityTaskPollers: dc.GetIntProperty(
				dynamicconfig.WorkerArchiverMaxConcurrentActivityTaskPollers,
				4,
			),
			MaxConcurrentWorkflowTaskPollers: dc.GetIntProperty(
				dynamicconfig.WorkerArchiverMaxConcurrentWorkflowTaskPollers,
				4,
			),

			ArchiverConcurrency: dc.GetIntProperty(
				dynamicconfig.WorkerArchiverConcurrency,
				50,
			),
			ArchivalsPerIteration: dc.GetIntProperty(
				dynamicconfig.WorkerArchivalsPerIteration,
				1000,
			),
			TimeLimitPerArchivalIteration: dc.GetDurationProperty(
				dynamicconfig.WorkerTimeLimitPerArchivalIteration,
				archiver.MaxArchivalIterationTimeout(),
			),
		},

		ParentCloseCfg: &parentclosepolicy.Config{
			MaxConcurrentActivityExecutionSize: dc.GetIntProperty(
				dynamicconfig.WorkerParentCloseMaxConcurrentActivityExecutionSize,
				1000,
			),
			MaxConcurrentWorkflowTaskExecutionSize: dc.GetIntProperty(
				dynamicconfig.WorkerParentCloseMaxConcurrentWorkflowTaskExecutionSize,
				1000,
			),
			MaxConcurrentActivityTaskPollers: dc.GetIntProperty(
				dynamicconfig.WorkerParentCloseMaxConcurrentActivityTaskPollers,
				4,
			),
			MaxConcurrentWorkflowTaskPollers: dc.GetIntProperty(
				dynamicconfig.WorkerParentCloseMaxConcurrentWorkflowTaskPollers,
				4,
			),
			NumParentClosePolicySystemWorkflows: dc.GetIntProperty(
				dynamicconfig.NumParentClosePolicySystemWorkflows,
				10,
			),
		},
		ScannerCfg: &scanner.Config{
			MaxConcurrentActivityExecutionSize: dc.GetIntProperty(
				dynamicconfig.WorkerScannerMaxConcurrentActivityExecutionSize,
				10,
			),
			MaxConcurrentWorkflowTaskExecutionSize: dc.GetIntProperty(
				dynamicconfig.WorkerScannerMaxConcurrentWorkflowTaskExecutionSize,
				10,
			),
			MaxConcurrentActivityTaskPollers: dc.GetIntProperty(
				dynamicconfig.WorkerScannerMaxConcurrentActivityTaskPollers,
				8,
			),
			MaxConcurrentWorkflowTaskPollers: dc.GetIntProperty(
				dynamicconfig.WorkerScannerMaxConcurrentWorkflowTaskPollers,
				8,
			),

			PersistenceMaxQPS: dc.GetIntProperty(
				dynamicconfig.ScannerPersistenceMaxQPS,
				100,
			),
			Persistence: persistenceConfig,
			TaskQueueScannerEnabled: dc.GetBoolProperty(
				dynamicconfig.TaskQueueScannerEnabled,
				true,
			),
			HistoryScannerEnabled: dc.GetBoolProperty(
				dynamicconfig.HistoryScannerEnabled,
				true,
			),
			ExecutionsScannerEnabled: dc.GetBoolProperty(
				dynamicconfig.ExecutionsScannerEnabled,
				false,
			),
			HistoryScannerDataMinAge: dc.GetDurationProperty(
				dynamicconfig.HistoryScannerDataMinAge,
				60*24*time.Hour,
			),
			HistoryScannerVerifyRetention: dc.GetBoolProperty(
				dynamicconfig.HistoryScannerVerifyRetention,
				false,
			),
			ExecutionScannerPerHostQPS: dc.GetIntProperty(
				dynamicconfig.ExecutionScannerPerHostQPS,
				10,
			),
			ExecutionScannerPerShardQPS: dc.GetIntProperty(
				dynamicconfig.ExecutionScannerPerShardQPS,
				1,
			),
			ExecutionDataDurationBuffer: dc.GetDurationProperty(
				dynamicconfig.ExecutionDataDurationBuffer,
				time.Hour*24*90,
			),
			ExecutionScannerWorkerCount: dc.GetIntProperty(
				dynamicconfig.ExecutionScannerWorkerCount,
				8,
			),
		},
		EnableBatcher:      dc.GetBoolProperty(dynamicconfig.EnableBatcher, true),
		BatcherRPS:         dc.GetIntPropertyFilteredByNamespace(dynamicconfig.BatcherRPS, batcher.DefaultRPS),
		BatcherConcurrency: dc.GetIntPropertyFilteredByNamespace(dynamicconfig.BatcherConcurrency, batcher.DefaultConcurrency),
		EnableParentClosePolicyWorker: dc.GetBoolProperty(
			dynamicconfig.EnableParentClosePolicyWorker,
			true,
		),
		PerNamespaceWorkerCount: dc.GetIntPropertyFilteredByNamespace(
			dynamicconfig.WorkerPerNamespaceWorkerCount,
			1,
		),
		ThrottledLogRPS: dc.GetIntProperty(
			dynamicconfig.WorkerThrottledLogRPS,
			20,
		),
		PersistenceMaxQPS: dc.GetIntProperty(
			dynamicconfig.WorkerPersistenceMaxQPS,
			500,
		),
		PersistenceGlobalMaxQPS: dc.GetIntProperty(
			dynamicconfig.WorkerPersistenceGlobalMaxQPS,
			0,
		),
		PersistenceNamespaceMaxQPS: dc.GetIntPropertyFilteredByNamespace(
			dynamicconfig.WorkerPersistenceNamespaceMaxQPS,
			0,
		),
		EnablePersistencePriorityRateLimiting: dc.GetBoolProperty(
			dynamicconfig.WorkerEnablePersistencePriorityRateLimiting,
			true,
		),

		StandardVisibilityPersistenceMaxReadQPS:   dc.GetIntProperty(dynamicconfig.StandardVisibilityPersistenceMaxReadQPS, 9000),
		StandardVisibilityPersistenceMaxWriteQPS:  dc.GetIntProperty(dynamicconfig.StandardVisibilityPersistenceMaxWriteQPS, 9000),
		AdvancedVisibilityPersistenceMaxReadQPS:   dc.GetIntProperty(dynamicconfig.AdvancedVisibilityPersistenceMaxReadQPS, 9000),
		AdvancedVisibilityPersistenceMaxWriteQPS:  dc.GetIntProperty(dynamicconfig.AdvancedVisibilityPersistenceMaxWriteQPS, 9000),
		EnableReadVisibilityFromES:                dc.GetBoolPropertyFnWithNamespaceFilter(dynamicconfig.EnableReadVisibilityFromES, enableReadFromES),
		EnableReadFromSecondaryAdvancedVisibility: dc.GetBoolPropertyFnWithNamespaceFilter(dynamicconfig.EnableReadFromSecondaryAdvancedVisibility, false),
		VisibilityDisableOrderByClause:            dc.GetBoolProperty(dynamicconfig.VisibilityDisableOrderByClause, false),
	}
	return config
}

// Start is called to start the service
func (s *Service) Start() {
	if !atomic.CompareAndSwapInt32(
		&s.status,
		common.DaemonStatusInitialized,
		common.DaemonStatusStarted,
	) {
		return
	}

	s.logger.Info(
		"worker starting",
		tag.ComponentWorker,
	)

	s.metricsHandler.Counter(metrics.RestartCount).Record(1)

	s.clusterMetadata.Start()
	s.membershipMonitor.Start()
	s.namespaceRegistry.Start()

	hostInfo, err := s.membershipMonitor.WhoAmI()
	if err != nil {
		s.logger.Fatal(
			"fail to get host info from membership monitor",
			tag.Error(err),
		)
	}
	s.hostInfo = hostInfo

	// The service is now started up
	// seed the random generator once for this service
	rand.Seed(time.Now().UnixNano())

	s.ensureSystemNamespaceExists(context.TODO())
	s.startScanner()

	if s.clusterMetadata.IsGlobalNamespaceEnabled() {
		s.startReplicator()
	}
	if s.archivalMetadata.GetHistoryConfig().ClusterConfiguredForArchival() {
		s.startArchiver()
	}
	if s.config.EnableParentClosePolicyWorker() {
		s.startParentClosePolicyProcessor()
	}
	if s.config.EnableBatcher() {
		s.startBatcher()
	}

	s.workerManager.Start()
	s.perNamespaceWorkerManager.Start(
		// TODO: get these from fx instead of passing through Start
		s.hostInfo,
		s.workerServiceResolver,
	)

	s.logger.Info(
		"worker service started",
		tag.ComponentWorker,
		tag.Address(hostInfo.GetAddress()),
	)
	<-s.stopC
}

// Stop is called to stop the service
func (s *Service) Stop() {
	if !atomic.CompareAndSwapInt32(
		&s.status,
		common.DaemonStatusStarted,
		common.DaemonStatusStopped,
	) {
		return
	}

	close(s.stopC)

	s.scanner.Stop()
	s.perNamespaceWorkerManager.Stop()
	s.workerManager.Stop()
	s.namespaceRegistry.Stop()
	s.membershipMonitor.Stop()
	s.clusterMetadata.Stop()
	s.persistenceBean.Close()
	s.visibilityManager.Close()

	s.logger.Info(
		"worker service stopped",
		tag.ComponentWorker,
		tag.Address(s.hostInfo.GetAddress()),
	)
}

func (s *Service) startParentClosePolicyProcessor() {
	params := &parentclosepolicy.BootstrapParams{
		Config:           *s.config.ParentCloseCfg,
		SdkClientFactory: s.sdkClientFactory,
		MetricsClient:    s.metricsClient,
		Logger:           s.logger,
		ClientBean:       s.clientBean,
		CurrentCluster:   s.clusterMetadata.GetCurrentClusterName(),
	}
	processor := parentclosepolicy.New(params)
	if err := processor.Start(); err != nil {
		s.logger.Fatal(
			"error starting parentclosepolicy processor",
			tag.Error(err),
		)
	}
}

func (s *Service) startBatcher() {
	if err := batcher.New(
		s.metricsClient,
		s.logger,
		s.sdkClientFactory,
		s.config.BatcherRPS,
		s.config.BatcherConcurrency,
	).Start(); err != nil {
		s.logger.Fatal(
			"error starting batcher worker",
			tag.Error(err),
		)
	}
}

func (s *Service) initScanner() error {
	currentCluster := s.clusterMetadata.GetCurrentClusterName()
	adminClient, err := s.clientBean.GetRemoteAdminClient(currentCluster)
	if err != nil {
		return err
	}
	s.scanner = scanner.New(
		s.logger,
		s.config.ScannerCfg,
		s.sdkClientFactory,
		s.metricsClient,
		s.executionManager,
		s.taskManager,
		s.historyClient,
		adminClient,
		s.namespaceRegistry,
		s.workerFactory,
	)
	return nil
}

func (s *Service) startScanner() {
	if err := s.scanner.Start(); err != nil {
		s.logger.Fatal(
			"error starting scanner",
			tag.Error(err),
		)
	}
}

func (s *Service) startReplicator() {
	namespaceReplicationTaskExecutor := namespace.NewReplicationTaskExecutor(
		s.clusterMetadata.GetCurrentClusterName(),
		s.metadataManager,
		s.logger,
	)
	msgReplicator := replicator.NewReplicator(
		s.clusterMetadata,
		s.clientBean,
		s.logger,
		s.metricsClient,
		s.hostInfo,
		s.workerServiceResolver,
		s.namespaceReplicationQueue,
		namespaceReplicationTaskExecutor,
	)
	msgReplicator.Start()
}

func (s *Service) startArchiver() {
	historyClient := s.clientBean.GetHistoryClient()
	bc := &archiver.BootstrapContainer{
		MetricsClient:    s.metricsClient,
		Logger:           s.logger,
		HistoryV2Manager: s.executionManager,
		NamespaceCache:   s.namespaceRegistry,
		Config:           s.config.ArchiverConfig,
		ArchiverProvider: s.archiverProvider,
		SdkClientFactory: s.sdkClientFactory,
		HistoryClient:    historyClient,
	}
	clientWorker := archiver.NewClientWorker(bc)
	if err := clientWorker.Start(); err != nil {
		clientWorker.Stop()
		s.logger.Fatal(
			"failed to start archiver",
			tag.Error(err),
		)
	}
}

func (s *Service) ensureSystemNamespaceExists(
	ctx context.Context,
) {
	_, err := s.metadataManager.GetNamespace(ctx, &persistence.GetNamespaceRequest{Name: primitives.SystemLocalNamespace})
	switch err.(type) {
	case nil:
		// noop
	case *serviceerror.NamespaceNotFound:
		s.logger.Fatal(
			"temporal-system namespace does not exist",
			tag.Error(err),
		)
	default:
		s.logger.Fatal(
			"failed to verify if temporal system namespace exists",
			tag.Error(err),
		)
	}
}
