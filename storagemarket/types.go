package storagemarket

import (
	"context"
	"io"

	"github.com/filecoin-project/go-address"
	"github.com/filecoin-project/specs-actors/actors/abi"
	"github.com/filecoin-project/specs-actors/actors/builtin/market"
	"github.com/filecoin-project/specs-actors/actors/crypto"
	"github.com/filecoin-project/specs-actors/actors/runtime/exitcode"
	"github.com/ipfs/go-cid"
	cbor "github.com/ipfs/go-ipld-cbor"
	"github.com/libp2p/go-libp2p-core/peer"

	"github.com/filecoin-project/go-fil-markets/filestore"
	"github.com/filecoin-project/go-fil-markets/shared"
)

//go:generate cbor-gen-for ClientDeal MinerDeal Balance SignedStorageAsk StorageAsk StorageDeal DataRef

const DealProtocolID = "/fil/storage/mk/1.0.1"
const AskProtocolID = "/fil/storage/ask/1.0.1"

type Balance struct {
	Locked    abi.TokenAmount
	Available abi.TokenAmount
}

type StorageDealStatus = uint64

const (
	StorageDealUnknown = StorageDealStatus(iota)
	StorageDealProposalNotFound
	StorageDealProposalRejected
	StorageDealProposalAccepted
	StorageDealStaged
	StorageDealSealing
	StorageDealActive
	StorageDealFailing
	StorageDealNotFound

	// Internal

	StorageDealFundsEnsured        // Deposited funds as neccesary to create a deal, ready to move forward
	StorageDealValidating          // Verifying that deal parameters are good
	StorageDealTransferring        // Moving data
	StorageDealWaitingForData      // Manual transfer
	StorageDealVerifyData          // Verify transferred data - generate CAR / piece data
	StorageDealEnsureProviderFunds // Ensuring that provider collateral is sufficient
	StorageDealEnsureClientFunds   // Ensuring that client funds are sufficient
	StorageDealProviderFunding     // Waiting for funds to appear in Provider balance
	StorageDealClientFunding       // Waiting for funds to appear in Client balance
	StorageDealPublish             // Publishing deal to chain
	StorageDealPublishing          // Waiting for deal to appear on chain
	StorageDealError               // deal failed with an unexpected error
	StorageDealCompleted           // on provider side, indicates deal is active and info for retrieval is recorded
)

// DealStates maps StorageDealStatus codes to string names
var DealStates = map[StorageDealStatus]string{
	StorageDealUnknown:             "StorageDealUnknown",
	StorageDealProposalNotFound:    "StorageDealProposalNotFound",
	StorageDealProposalRejected:    "StorageDealProposalRejected",
	StorageDealProposalAccepted:    "StorageDealProposalAccepted",
	StorageDealStaged:              "StorageDealStaged",
	StorageDealSealing:             "StorageDealSealing",
	StorageDealActive:              "StorageDealActive",
	StorageDealFailing:             "StorageDealFailing",
	StorageDealNotFound:            "StorageDealNotFound",
	StorageDealFundsEnsured:        "StorageDealFundsEnsured",
	StorageDealValidating:          "StorageDealValidating",
	StorageDealTransferring:        "StorageDealTransferring",
	StorageDealWaitingForData:      "StorageDealWaitingForData",
	StorageDealVerifyData:          "StorageDealVerifyData",
	StorageDealEnsureProviderFunds: "StorageDealEnsureProviderFunds",
	StorageDealEnsureClientFunds:   "StorageDealEnsureClientFunds",
	StorageDealProviderFunding:     "StorageDealProviderFunding",
	StorageDealClientFunding:       "StorageDealClientFunding",
	StorageDealPublish:             "StorageDealPublish",
	StorageDealPublishing:          "StorageDealPublishing",
	StorageDealError:               "StorageDealError",
	StorageDealCompleted:           "StorageDealCompleted",
}

func init() {
	cbor.RegisterCborType(SignedStorageAsk{})
	cbor.RegisterCborType(StorageAsk{})
}

type SignedStorageAsk struct {
	Ask       *StorageAsk
	Signature *crypto.Signature
}

var SignedStorageAskUndefined = SignedStorageAsk{}

type StorageAsk struct {
	// Price per GiB / Epoch
	Price abi.TokenAmount

	MinPieceSize abi.PaddedPieceSize
	MaxPieceSize abi.PaddedPieceSize
	Miner        address.Address
	Timestamp    abi.ChainEpoch
	Expiry       abi.ChainEpoch
	SeqNo        uint64
}

// StorageAskOption allows custom configuration of a storage ask
type StorageAskOption func(*StorageAsk)

func MinPieceSize(minPieceSize abi.PaddedPieceSize) StorageAskOption {
	return func(sa *StorageAsk) {
		sa.MinPieceSize = minPieceSize
	}
}

func MaxPieceSize(maxPieceSize abi.PaddedPieceSize) StorageAskOption {
	return func(sa *StorageAsk) {
		sa.MaxPieceSize = maxPieceSize
	}
}

var StorageAskUndefined = StorageAsk{}

type MinerDeal struct {
	market.ClientDealProposal
	ProposalCid      cid.Cid
	AddFundsCid      *cid.Cid
	PublishCid       *cid.Cid
	Miner            peer.ID
	Client           peer.ID
	State            StorageDealStatus
	PiecePath        filestore.Path
	MetadataPath     filestore.Path
	ConnectionClosed bool
	Message          string

	Ref *DataRef

	DealID abi.DealID
}

type ProviderEvent uint64

const (
	// ProviderEventOpen indicates a new deal proposal has been received
	ProviderEventOpen ProviderEvent = iota

	// ProviderEventNodeErrored indicates an error happened talking to the node implementation
	ProviderEventNodeErrored

	// ProviderEventDealRejected happens when a deal proposal is rejected for not meeting criteria
	ProviderEventDealRejected

	// ProviderEventDealAccepted happens when a deal is accepted based on provider criteria
	ProviderEventDealAccepted

	// ProviderEventWaitingForManualData happens when an offline deal proposal is accepted,
	// meaning the provider must wait until it receives data manually
	ProviderEventWaitingForManualData

	// ProviderEventInsufficientFunds indicates not enough funds available for a deal
	ProviderEventInsufficientFunds

	// ProviderEventFundingInitiated indicates provider collateral funding has been initiated
	ProviderEventFundingInitiated

	// ProviderEventFunded indicates provider collateral has appeared in the storage market balance
	ProviderEventFunded

	// ProviderEventDataTransferFailed happens when an error occurs transferring data
	ProviderEventDataTransferFailed

	// ProviderEventDataTransferInitiated happens when a data transfer starts
	ProviderEventDataTransferInitiated

	// ProviderEventDataTransferCompleted happens when a data transfer is successful
	ProviderEventDataTransferCompleted

	// ProviderEventManualDataReceived happens when data is received manually for an offline deal
	ProviderEventManualDataReceived

	// ProviderEventGeneratePieceCIDFailed happens when generating a piece cid from received data errors
	ProviderEventGeneratePieceCIDFailed

	// ProviderEventVerifiedData happens when received data is verified as matching the pieceCID in a deal proposal
	ProviderEventVerifiedData

	// ProviderEventSendResponseFailed happens when a response cannot be sent to a deal
	ProviderEventSendResponseFailed

	// ProviderEventDealPublishInitiated happens when a provider has sent a PublishStorageDeals message to the chain
	ProviderEventDealPublishInitiated

	// ProviderEventDealPublished happens when a deal is successfully published
	ProviderEventDealPublished

	// ProviderEventDealPublishError happens when PublishStorageDeals returns a non-ok exit code
	ProviderEventDealPublishError

	// ProviderEventFileStoreErrored happens when an error occurs accessing the filestore
	ProviderEventFileStoreErrored

	// ProviderEventDealHandoffFailed happens when an error occurs handing off a deal with OnDealComplete
	ProviderEventDealHandoffFailed

	// ProviderEventDealHandedOff happens when a deal is successfully handed off to the node for processing in a sector
	ProviderEventDealHandedOff

	// ProviderEventDealActivationFailed happens when an error occurs activating a deal
	ProviderEventDealActivationFailed

	// ProviderEventUnableToLocatePiece happens when an attempt to learn the location of a piece from
	// the node fails
	ProviderEventUnableToLocatePiece

	// ProviderEventDealActivated happens when a deal is successfully activated and commited to a sector
	ProviderEventDealActivated

	// ProviderEventPieceStoreErrored happens when an attempt to save data in the piece store errors
	ProviderEventPieceStoreErrored

	// ProviderEventReadMetadataErrored happens when an error occurs reading recorded piece metadata
	ProviderEventReadMetadataErrored

	// ProviderEventDealCompleted happens when a deal completes successfully
	ProviderEventDealCompleted

	// ProviderEventFailed indicates a deal has failed and should no longer be processed
	ProviderEventFailed

	// ProviderEventRestart is used to resume the deal after a state machine shutdown
	ProviderEventRestart
)

// ProviderEvents maps provider event codes to string names
var ProviderEvents = map[ProviderEvent]string{
	ProviderEventOpen:                   "ProviderEventOpen",
	ProviderEventNodeErrored:            "ProviderEventNodeErrored",
	ProviderEventDealRejected:           "ProviderEventDealRejected",
	ProviderEventDealAccepted:           "ProviderEventDealAccepted",
	ProviderEventWaitingForManualData:   "ProviderEventWaitingForManualData",
	ProviderEventInsufficientFunds:      "ProviderEventInsufficientFunds",
	ProviderEventFundingInitiated:       "ProviderEventFundingInitiated",
	ProviderEventFunded:                 "ProviderEventFunded",
	ProviderEventDataTransferFailed:     "ProviderEventDataTransferFailed",
	ProviderEventDataTransferInitiated:  "ProviderEventDataTransferInitiated",
	ProviderEventDataTransferCompleted:  "ProviderEventDataTransferCompleted",
	ProviderEventManualDataReceived:     "ProviderEventManualDataReceived",
	ProviderEventGeneratePieceCIDFailed: "ProviderEventGeneratePieceCIDFailed",
	ProviderEventVerifiedData:           "ProviderEventVerifiedData",
	ProviderEventSendResponseFailed:     "ProviderEventSendResponseFailed",
	ProviderEventDealPublishInitiated:   "ProviderEventDealPublishInitiated",
	ProviderEventDealPublished:          "ProviderEventDealPublished",
	ProviderEventDealPublishError:       "ProviderEventDealPublishError",
	ProviderEventFileStoreErrored:       "ProviderEventFileStoreErrored",
	ProviderEventDealHandoffFailed:      "ProviderEventDealHandoffFailed",
	ProviderEventDealHandedOff:          "ProviderEventDealHandedOff",
	ProviderEventDealActivationFailed:   "ProviderEventDealActivationFailed",
	ProviderEventUnableToLocatePiece:    "ProviderEventUnableToLocatePiece",
	ProviderEventDealActivated:          "ProviderEventDealActivated",
	ProviderEventPieceStoreErrored:      "ProviderEventPieceStoreErrored",
	ProviderEventReadMetadataErrored:    "ProviderEventReadMetadataErrored",
	ProviderEventDealCompleted:          "ProviderEventDealCompleted",
	ProviderEventFailed:                 "ProviderEventFailed",
	ProviderEventRestart:                "ProviderEventRestart",
}

type ClientDeal struct {
	market.ClientDealProposal
	ProposalCid      cid.Cid
	AddFundsCid      *cid.Cid
	State            StorageDealStatus
	Miner            peer.ID
	MinerWorker      address.Address
	DealID           abi.DealID
	DataRef          *DataRef
	Message          string
	PublishMessage   *cid.Cid
	ConnectionClosed bool
}

type ClientEvent uint64

const (
	// ClientEventOpen indicates a new deal was started
	ClientEventOpen ClientEvent = iota

	// ClientEventEnsureFundsFailed happens when attempting to ensure the client has enough funds available fails
	ClientEventEnsureFundsFailed

	// ClientEventFundingInitiated happens when a client has sent a message adding funds to its balance
	ClientEventFundingInitiated

	// ClientEventFundsEnsured happens when a client successfully ensures it has funds for a deal
	ClientEventFundsEnsured

	// ClientEventWriteProposalFailed indicates an attempt to send a deal proposal to a provider failed
	ClientEventWriteProposalFailed

	// ClientEventDealProposed happens when a new proposal is sent to a provider
	ClientEventDealProposed

	// ClientEventDealStreamLookupErrored the deal stream for a deal could not be found
	ClientEventDealStreamLookupErrored

	// ClientEventReadResponseFailed means a network error occurred reading a deal response
	ClientEventReadResponseFailed

	// ClientEventResponseVerificationFailed means a response was not verified
	ClientEventResponseVerificationFailed

	// ClientEventResponseDealDidNotMatch means a response was sent for the wrong deal
	ClientEventResponseDealDidNotMatch

	// ClientEventStreamCloseError happens when an attempt to close a deals stream fails
	ClientEventStreamCloseError

	// ClientEventDealRejected happens when the provider does not accept a deal
	ClientEventDealRejected

	// ClientEventDealAccepted happens when a client receives a response accepting a deal from a provider
	ClientEventDealAccepted

	// ClientEventDealPublishFailed happens when a client cannot verify a deal was published
	ClientEventDealPublishFailed

	// ClientEventDealPublished happens when a deal is successfully published
	ClientEventDealPublished

	// ClientEventDealActivationFailed happens when a client cannot verify a deal was activated
	ClientEventDealActivationFailed

	// ClientEventDealActivated happens when a deal is successfully activated
	ClientEventDealActivated

	// ClientEventFailed happens when a deal terminates in failure
	ClientEventFailed

	// ClientEventRestart is used to resume the deal after a state machine shutdown
	ClientEventRestart
)

// ClientEvents maps client event codes to string names
var ClientEvents = map[ClientEvent]string{
	ClientEventOpen:                       "ClientEventOpen",
	ClientEventEnsureFundsFailed:          "ClientEventEnsureFundsFailed",
	ClientEventFundingInitiated:           "ClientEventFundingInitiated",
	ClientEventFundsEnsured:               "ClientEventFundsEnsured",
	ClientEventWriteProposalFailed:        "ClientEventWriteProposalFailed",
	ClientEventDealProposed:               "ClientEventDealProposed",
	ClientEventDealStreamLookupErrored:    "ClientEventDealStreamLookupErrored",
	ClientEventReadResponseFailed:         "ClientEventReadResponseFailed",
	ClientEventResponseVerificationFailed: "ClientEventResponseVerificationFailed",
	ClientEventResponseDealDidNotMatch:    "ClientEventResponseDealDidNotMatch",
	ClientEventStreamCloseError:           "ClientEventStreamCloseError",
	ClientEventDealRejected:               "ClientEventDealRejected",
	ClientEventDealAccepted:               "ClientEventDealAccepted",
	ClientEventDealPublishFailed:          "ClientEventDealPublishFailed",
	ClientEventDealPublished:              "ClientEventDealPublished",
	ClientEventDealActivationFailed:       "ClientEventDealActivationFailed",
	ClientEventDealActivated:              "ClientEventDealActivated",
	ClientEventFailed:                     "ClientEventFailed",
}

// StorageDeal is a local combination of a proposal and a current deal state
type StorageDeal struct {
	market.DealProposal
	market.DealState
}

type DealSectorCommittedCallback func(err error)
type FundsAddedCallback func(err error)
type DealsPublishedCallback func(err error)
type MessagePublishedCallback func(mcid cid.Cid, err error)

// Subscriber is a callback that is called when events are emitted
type ProviderSubscriber func(event ProviderEvent, deal MinerDeal)
type ClientSubscriber func(event ClientEvent, deal ClientDeal)

// StorageProvider is the interface provided for storage providers
type StorageProvider interface {
	Start(ctx context.Context) error

	Stop() error

	AddAsk(price abi.TokenAmount, duration abi.ChainEpoch, options ...StorageAskOption) error

	// ListAsks lists current asks
	ListAsks(addr address.Address) []*SignedStorageAsk

	// ListDeals lists on-chain deals associated with this storage provider
	ListDeals(ctx context.Context) ([]StorageDeal, error)

	// ListLocalDeals lists deals processed by this storage provider
	ListLocalDeals() ([]MinerDeal, error)

	// AddStorageCollateral adds storage collateral
	AddStorageCollateral(ctx context.Context, amount abi.TokenAmount) error

	// GetStorageCollateral returns the current collateral balance
	GetStorageCollateral(ctx context.Context) (Balance, error)

	ImportDataForDeal(ctx context.Context, propCid cid.Cid, data io.Reader) error

	SubscribeToEvents(subscriber ProviderSubscriber) shared.Unsubscribe
}

type StorageFunds interface {
	// Adds funds with the StorageMinerActor for a storage participant.  Used by both providers and clients.
	AddFunds(ctx context.Context, addr address.Address, amount abi.TokenAmount) (cid.Cid, error)

	// Ensures that a storage market participant has a certain amount of available funds
	// If additional funds are needed, they will be sent from the 'wallet' address
	// callback is immediately called if sufficient funds are available
	EnsureFunds(ctx context.Context, addr, wallet address.Address, amount abi.TokenAmount, tok shared.TipSetToken) (cid.Cid, error)

	// GetBalance returns locked/unlocked for a storage participant.  Used by both providers and clients.
	GetBalance(ctx context.Context, addr address.Address, tok shared.TipSetToken) (Balance, error)

	// Verify a signature against an address + data
	VerifySignature(ctx context.Context, signature crypto.Signature, signer address.Address, plaintext []byte, tok shared.TipSetToken) (bool, error)

	WaitForMessage(ctx context.Context, mcid cid.Cid, onCompletion func(exitcode.ExitCode, []byte, error) error) error
}

// Node dependencies for a StorageProvider
type StorageProviderNode interface {
	StorageFunds

	GetChainHead(ctx context.Context) (shared.TipSetToken, abi.ChainEpoch, error)

	// Publishes deal on chain, returns the message cid, but does not wait for message to appear
	PublishDeals(ctx context.Context, deal MinerDeal) (cid.Cid, error)

	// ListProviderDeals lists all deals associated with a storage provider
	ListProviderDeals(ctx context.Context, addr address.Address, tok shared.TipSetToken) ([]StorageDeal, error)

	// Called when a deal is complete and on chain, and data has been transferred and is ready to be added to a sector
	OnDealComplete(ctx context.Context, deal MinerDeal, pieceSize abi.UnpaddedPieceSize, pieceReader io.Reader) error

	// returns the worker address associated with a miner
	GetMinerWorkerAddress(ctx context.Context, addr address.Address, tok shared.TipSetToken) (address.Address, error)

	// Signs bytes
	SignBytes(ctx context.Context, signer address.Address, b []byte) (*crypto.Signature, error)

	OnDealSectorCommitted(ctx context.Context, provider address.Address, dealID abi.DealID, cb DealSectorCommittedCallback) error

	LocatePieceForDealWithinSector(ctx context.Context, dealID abi.DealID, tok shared.TipSetToken) (sectorID uint64, offset uint64, length uint64, err error)
}

// Node dependencies for a StorageClient
type StorageClientNode interface {
	StorageFunds

	GetChainHead(ctx context.Context) (shared.TipSetToken, abi.ChainEpoch, error)

	// ListClientDeals lists all on-chain deals associated with a storage client
	ListClientDeals(ctx context.Context, addr address.Address, tok shared.TipSetToken) ([]StorageDeal, error)

	// GetProviderInfo returns information about a single storage provider
	//GetProviderInfo(stateId StateID, addr Address) *StorageProviderInfo

	// GetStorageProviders returns information about known miners
	ListStorageProviders(ctx context.Context, tok shared.TipSetToken) ([]*StorageProviderInfo, error)

	// Subscribes to storage market actor state changes for a given address.
	// TODO: Should there be a timeout option for this?  In the case that we are waiting for funds to be deposited and it never happens?
	//SubscribeStorageMarketEvents(addr Address, handler StorageMarketEventHandler) (SubID, error)

	// Cancels a subscription
	//UnsubscribeStorageMarketEvents(subId SubID)
	ValidatePublishedDeal(ctx context.Context, deal ClientDeal) (abi.DealID, error)

	// SignProposal signs a proposal
	SignProposal(ctx context.Context, signer address.Address, proposal market.DealProposal) (*market.ClientDealProposal, error)

	GetDefaultWalletAddress(ctx context.Context) (address.Address, error)

	OnDealSectorCommitted(ctx context.Context, provider address.Address, dealID abi.DealID, cb DealSectorCommittedCallback) error

	ValidateAskSignature(ctx context.Context, ask *SignedStorageAsk, tok shared.TipSetToken) (bool, error)
}

type StorageClientProofs interface {
	//GeneratePieceCommitment(piece io.Reader, pieceSize uint64) (CommP, error)
}

// Closely follows the MinerInfo struct in the spec
type StorageProviderInfo struct {
	Address    address.Address // actor address
	Owner      address.Address
	Worker     address.Address // signs messages
	SectorSize uint64
	PeerID     peer.ID
	// probably more like how much storage power, available collateral etc
}

type ProposeStorageDealResult struct {
	ProposalCid cid.Cid
}

const (
	TTGraphsync = "graphsync"
	TTManual    = "manual"
)

type DataRef struct {
	TransferType string
	Root         cid.Cid

	PieceCid  *cid.Cid              // Optional for non-manual transfer, will be recomputed from the data if not given
	PieceSize abi.UnpaddedPieceSize // Optional for non-manual transfer, will be recomputed from the data if not given
}

// The interface provided by the module to the outside world for storage clients.
type StorageClient interface {
	Run(ctx context.Context)

	Stop()

	// ListProviders queries chain state and returns active storage providers
	ListProviders(ctx context.Context) (<-chan StorageProviderInfo, error)

	// ListDeals lists on-chain deals associated with this storage client
	ListDeals(ctx context.Context, addr address.Address) ([]StorageDeal, error)

	// ListLocalDeals lists deals initiated by this storage client
	ListLocalDeals(ctx context.Context) ([]ClientDeal, error)

	// GetLocalDeal lists deals that are in progress or rejected
	GetLocalDeal(ctx context.Context, cid cid.Cid) (ClientDeal, error)

	// GetAsk returns the current ask for a storage provider
	GetAsk(ctx context.Context, info StorageProviderInfo) (*SignedStorageAsk, error)

	//// FindStorageOffers lists providers and queries them to find offers that satisfy some criteria based on price, duration, etc.
	//FindStorageOffers(criteria AskCriteria, limit uint) []*StorageOffer

	// ProposeStorageDeal initiates deal negotiation with a Storage Provider
	ProposeStorageDeal(ctx context.Context, addr address.Address, info *StorageProviderInfo, data *DataRef, startEpoch abi.ChainEpoch, endEpoch abi.ChainEpoch, price abi.TokenAmount, collateral abi.TokenAmount, rt abi.RegisteredProof) (*ProposeStorageDealResult, error)

	// GetPaymentEscrow returns the current funds available for deal payment
	GetPaymentEscrow(ctx context.Context, addr address.Address) (Balance, error)

	// AddStorageCollateral adds storage collateral
	AddPaymentEscrow(ctx context.Context, addr address.Address, amount abi.TokenAmount) error

	SubscribeToEvents(subscriber ClientSubscriber) shared.Unsubscribe
}
