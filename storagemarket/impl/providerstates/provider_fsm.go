package providerstates

import (
	"github.com/filecoin-project/go-statemachine/fsm"
	"github.com/filecoin-project/specs-actors/actors/abi"
	"github.com/ipfs/go-cid"
	"golang.org/x/xerrors"

	"github.com/filecoin-project/go-fil-markets/filestore"
	"github.com/filecoin-project/go-fil-markets/storagemarket"
)

// ProviderEvents are the events that can happen in a storage provider
var ProviderEvents = fsm.Events{
	fsm.Event(storagemarket.ProviderEventOpen).From(storagemarket.StorageDealUnknown).To(storagemarket.StorageDealValidating),
	fsm.Event(storagemarket.ProviderEventNodeErrored).FromAny().To(storagemarket.StorageDealFailing).
		Action(func(deal *storagemarket.MinerDeal, err error) error {
			deal.Message = xerrors.Errorf("error calling node: %w", err).Error()
			return nil
		}),
	fsm.Event(storagemarket.ProviderEventDealRejected).
		FromMany(storagemarket.StorageDealValidating, storagemarket.StorageDealVerifyData).To(storagemarket.StorageDealFailing).
		Action(func(deal *storagemarket.MinerDeal, err error) error {
			deal.Message = xerrors.Errorf("deal rejected: %w", err).Error()
			return nil
		}),
	fsm.Event(storagemarket.ProviderEventDealAccepted).
		From(storagemarket.StorageDealValidating).To(storagemarket.StorageDealProposalAccepted),
	fsm.Event(storagemarket.ProviderEventWaitingForManualData).
		From(storagemarket.StorageDealProposalAccepted).To(storagemarket.StorageDealWaitingForData),
	fsm.Event(storagemarket.ProviderEventDataTransferFailed).
		FromMany(storagemarket.StorageDealProposalAccepted, storagemarket.StorageDealTransferring).To(storagemarket.StorageDealFailing).
		Action(func(deal *storagemarket.MinerDeal, err error) error {
			deal.Message = xerrors.Errorf("error transferring data: %w", err).Error()
			return nil
		}),
	fsm.Event(storagemarket.ProviderEventDataTransferInitiated).
		From(storagemarket.StorageDealProposalAccepted).To(storagemarket.StorageDealTransferring),
	fsm.Event(storagemarket.ProviderEventDataTransferCompleted).
		From(storagemarket.StorageDealTransferring).To(storagemarket.StorageDealVerifyData),
	fsm.Event(storagemarket.ProviderEventGeneratePieceCIDFailed).
		From(storagemarket.StorageDealVerifyData).To(storagemarket.StorageDealFailing).
		Action(func(deal *storagemarket.MinerDeal, err error) error {
			deal.Message = xerrors.Errorf("generating piece committment: %w", err).Error()
			return nil
		}),
	fsm.Event(storagemarket.ProviderEventVerifiedData).
		FromMany(storagemarket.StorageDealVerifyData, storagemarket.StorageDealWaitingForData).To(storagemarket.StorageDealEnsureProviderFunds).
		Action(func(deal *storagemarket.MinerDeal, path filestore.Path, metadataPath filestore.Path) error {
			deal.PiecePath = path
			deal.MetadataPath = metadataPath
			return nil
		}),
	fsm.Event(storagemarket.ProviderEventFundingInitiated).
		From(storagemarket.StorageDealEnsureProviderFunds).To(storagemarket.StorageDealProviderFunding).
		Action(func(deal *storagemarket.MinerDeal, mcid cid.Cid) error {
			deal.AddFundsCid = &mcid
			return nil
		}),
	fsm.Event(storagemarket.ProviderEventFunded).
		FromMany(storagemarket.StorageDealProviderFunding, storagemarket.StorageDealEnsureProviderFunds).To(storagemarket.StorageDealPublish),
	fsm.Event(storagemarket.ProviderEventDealPublishInitiated).
		From(storagemarket.StorageDealPublish).To(storagemarket.StorageDealPublishing).
		Action(func(deal *storagemarket.MinerDeal, publishCid cid.Cid) error {
			deal.PublishCid = &publishCid
			return nil
		}),
	fsm.Event(storagemarket.ProviderEventDealPublishError).
		From(storagemarket.StorageDealPublishing).To(storagemarket.StorageDealFailing).
		Action(func(deal *storagemarket.MinerDeal, err error) error {
			deal.Message = xerrors.Errorf("PublishStorageDeal error: %w", err).Error()
			return nil
		}),
	fsm.Event(storagemarket.ProviderEventSendResponseFailed).
		FromMany(storagemarket.StorageDealPublishing, storagemarket.StorageDealFailing).To(storagemarket.StorageDealError).
		Action(func(deal *storagemarket.MinerDeal, err error) error {
			deal.Message = xerrors.Errorf("sending response to deal: %w", err).Error()
			return nil
		}),
	fsm.Event(storagemarket.ProviderEventDealPublished).
		From(storagemarket.StorageDealPublishing).To(storagemarket.StorageDealStaged).
		Action(func(deal *storagemarket.MinerDeal, dealID abi.DealID) error {
			deal.ConnectionClosed = true
			deal.DealID = dealID
			return nil
		}),
	fsm.Event(storagemarket.ProviderEventFileStoreErrored).
		FromMany(storagemarket.StorageDealStaged, storagemarket.StorageDealSealing, storagemarket.StorageDealActive).To(storagemarket.StorageDealFailing).
		Action(func(deal *storagemarket.MinerDeal, err error) error {
			deal.Message = xerrors.Errorf("accessing file store: %w", err).Error()
			return nil
		}),
	fsm.Event(storagemarket.ProviderEventDealHandoffFailed).From(storagemarket.StorageDealStaged).To(storagemarket.StorageDealFailing).
		Action(func(deal *storagemarket.MinerDeal, err error) error {
			deal.Message = xerrors.Errorf("handing off deal to node: %w", err).Error()
			return nil
		}),
	fsm.Event(storagemarket.ProviderEventDealHandedOff).From(storagemarket.StorageDealStaged).To(storagemarket.StorageDealSealing),
	fsm.Event(storagemarket.ProviderEventDealActivationFailed).
		From(storagemarket.StorageDealSealing).To(storagemarket.StorageDealFailing).
		Action(func(deal *storagemarket.MinerDeal, err error) error {
			deal.Message = xerrors.Errorf("error activating deal: %w", err).Error()
			return nil
		}),
	fsm.Event(storagemarket.ProviderEventDealActivated).From(storagemarket.StorageDealSealing).To(storagemarket.StorageDealActive),
	fsm.Event(storagemarket.ProviderEventPieceStoreErrored).From(storagemarket.StorageDealActive).To(storagemarket.StorageDealFailing).
		Action(func(deal *storagemarket.MinerDeal, err error) error {
			deal.Message = xerrors.Errorf("accessing piece store: %w", err).Error()
			return nil
		}),
	fsm.Event(storagemarket.ProviderEventDealCompleted).From(storagemarket.StorageDealActive).To(storagemarket.StorageDealCompleted),
	fsm.Event(storagemarket.ProviderEventUnableToLocatePiece).From(storagemarket.StorageDealActive).To(storagemarket.StorageDealFailing).
		Action(func(deal *storagemarket.MinerDeal, dealID abi.DealID, err error) error {
			deal.Message = xerrors.Errorf("locating piece for deal ID %d in sector: %w", deal.DealID, err).Error()
			return nil
		}),
	fsm.Event(storagemarket.ProviderEventReadMetadataErrored).From(storagemarket.StorageDealActive).To(storagemarket.StorageDealFailing).
		Action(func(deal *storagemarket.MinerDeal, err error) error {
			deal.Message = xerrors.Errorf("error reading piece metadata: %w", err).Error()
			return nil
		}),
	fsm.Event(storagemarket.ProviderEventFailed).From(storagemarket.StorageDealFailing).To(storagemarket.StorageDealError),
}

// ProviderStateEntryFuncs are the handlers for different states in a storage client
var ProviderStateEntryFuncs = fsm.StateEntryFuncs{
	storagemarket.StorageDealValidating:          ValidateDealProposal,
	storagemarket.StorageDealProposalAccepted:    TransferData,
	storagemarket.StorageDealVerifyData:          VerifyData,
	storagemarket.StorageDealEnsureProviderFunds: EnsureProviderFunds,
	storagemarket.StorageDealProviderFunding:     WaitForFunding,
	storagemarket.StorageDealPublish:             PublishDeal,
	storagemarket.StorageDealPublishing:          WaitForPublish,
	storagemarket.StorageDealStaged:              HandoffDeal,
	storagemarket.StorageDealSealing:             VerifyDealActivated,
	storagemarket.StorageDealActive:              RecordPieceInfo,
	storagemarket.StorageDealFailing:             FailDeal,
}

var ProviderFinalityStates = []fsm.StateKey{
	storagemarket.StorageDealError,
	storagemarket.StorageDealCompleted,
}
