package contentmgr

import (
	"sync"

	"github.com/application-research/estuary/config"
	"github.com/application-research/estuary/drpc"
	"github.com/application-research/estuary/miner"
	"github.com/application-research/estuary/node"
	"github.com/application-research/estuary/pinner"
	"github.com/application-research/estuary/util"
	"github.com/application-research/filclient"
	"github.com/filecoin-project/lotus/api"
	lru "github.com/hashicorp/golang-lru"
	"github.com/ipfs/go-cid"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/trace"
	"go.uber.org/zap"
	"gorm.io/gorm"
)

type ContentManager struct {
	db                   *gorm.DB
	api                  api.Gateway
	filClient            *filclient.FilClient
	node                 *node.Node
	cfg                  *config.Estuary
	tracer               trace.Tracer
	blockstore           node.EstuaryBlockstore
	notifyBlockstore     *node.NotifyBlockstore
	toCheck              chan uint
	queueMgr             *queueManager
	retrLk               sync.Mutex
	retrievalsInProgress map[uint]*util.RetrievalProgress
	contentLk            sync.RWMutex
	bucketLk             sync.Mutex
	buckets              map[uint][]*ContentStagingZone
	dealDisabledLk       sync.Mutex
	isDealMakingDisabled bool
	PinMgr               *pinner.PinManager
	ShuttlesLk           sync.Mutex
	Shuttles             map[string]*ShuttleConnection
	remoteTransferStatus *lru.ARCCache
	inflightCids         map[cid.Cid]uint
	inflightCidsLk       sync.Mutex
	IncomingRPCMessages  chan *drpc.Message
	minerManager         miner.IMinerManager
	log                  *zap.SugaredLogger
}

func NewContentManager(db *gorm.DB, api api.Gateway, fc *filclient.FilClient, tbs *util.TrackingBlockstore, pinmgr *pinner.PinManager, nd *node.Node, cfg *config.Estuary, minerManager miner.IMinerManager, log *zap.SugaredLogger) (*ContentManager, error) {
	cache, err := lru.NewARC(50000)
	if err != nil {
		return nil, err
	}

	cm := &ContentManager{
		cfg:                  cfg,
		db:                   db,
		api:                  api,
		filClient:            fc,
		blockstore:           tbs.Under().(node.EstuaryBlockstore),
		node:                 nd,
		notifyBlockstore:     nd.NotifBlockstore,
		toCheck:              make(chan uint, 100000),
		retrievalsInProgress: make(map[uint]*util.RetrievalProgress),
		buckets:              make(map[uint][]*ContentStagingZone),
		PinMgr:               pinmgr,
		remoteTransferStatus: cache,
		Shuttles:             make(map[string]*ShuttleConnection),
		inflightCids:         make(map[cid.Cid]uint),
		isDealMakingDisabled: cfg.Deal.IsDisabled,
		tracer:               otel.Tracer("replicator"),
		IncomingRPCMessages:  make(chan *drpc.Message, cfg.RPCMessage.IncomingQueueSize),
		minerManager:         minerManager,
		log:                  log,
	}

	cm.queueMgr = newQueueManager(func(c uint) {
		cm.ToCheck(c)
	})
	return cm, nil
}
