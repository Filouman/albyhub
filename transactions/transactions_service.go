package transactions

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/getAlby/hub/constants"
	"github.com/getAlby/hub/db"
	"github.com/getAlby/hub/db/queries"
	"github.com/getAlby/hub/events"
	"github.com/getAlby/hub/lnclient"
	"github.com/getAlby/hub/logger"
	decodepay "github.com/nbd-wtf/ln-decodepay"
	"github.com/sirupsen/logrus"
	"gorm.io/datatypes"
	"gorm.io/gorm"
)

type transactionsService struct {
	db             *gorm.DB
	eventPublisher events.EventPublisher
}

type TransactionsService interface {
	events.EventSubscriber
	MakeInvoice(ctx context.Context, amount uint64, description string, descriptionHash string, expiry uint64, metadata map[string]interface{}, lnClient lnclient.LNClient, appId *uint, requestEventId *uint) (*Transaction, error)
	LookupTransaction(ctx context.Context, paymentHash string, transactionType *string, lnClient lnclient.LNClient, appId *uint) (*Transaction, error)
	ListTransactions(ctx context.Context, from, until, limit, offset uint64, unpaidOutgoing bool, unpaidIncoming bool, transactionType *string, lnClient lnclient.LNClient, appId *uint) (transactions []Transaction, err error)
	SendPaymentSync(ctx context.Context, payReq string, metadata map[string]interface{}, lnClient lnclient.LNClient, appId *uint, requestEventId *uint) (*Transaction, error)
	SendKeysend(ctx context.Context, amount uint64, destination string, customRecords []lnclient.TLVRecord, preimage string, lnClient lnclient.LNClient, appId *uint, requestEventId *uint) (*Transaction, error)
}

const (
	BoostagramTlvType = 7629169
	WhatsatTlvType    = 34349334
	CustomKeyTlvType  = 696969
)

type Transaction = db.Transaction

type Boostagram struct {
	AppName        string         `json:"app_name"`
	Name           string         `json:"name"`
	Podcast        string         `json:"podcast"`
	URL            string         `json:"url"`
	Episode        StringOrNumber `json:"episode,omitempty"`
	FeedId         StringOrNumber `json:"feedID,omitempty"`
	ItemId         StringOrNumber `json:"itemID,omitempty"`
	Timestamp      int64          `json:"ts,omitempty"`
	Message        string         `json:"message,omitempty"`
	SenderId       StringOrNumber `json:"sender_id"`
	SenderName     string         `json:"sender_name"`
	Time           string         `json:"time"`
	Action         string         `json:"action"`
	ValueMsatTotal int64          `json:"value_msat_total"`
}

type StringOrNumber struct {
	StringData string
	NumberData int64
}

func (sn *StringOrNumber) UnmarshalJSON(data []byte) error {
	if err := json.Unmarshal(data, &sn.StringData); err == nil {
		return nil
	}

	if err := json.Unmarshal(data, &sn.NumberData); err == nil {
		return nil
	}

	return fmt.Errorf("cannot unmarshal %s into StringOrNumber type", data)
}

func (sn StringOrNumber) String() string {
	if sn.StringData != "" {
		return sn.StringData
	}
	return fmt.Sprintf("%d", sn.NumberData)
}

type notFoundError struct {
}

func NewNotFoundError() error {
	return &notFoundError{}
}

func (err *notFoundError) Error() string {
	return "The transaction requested was not found"
}

type insufficientBalanceError struct {
}

func NewInsufficientBalanceError() error {
	return &insufficientBalanceError{}
}

func (err *insufficientBalanceError) Error() string {
	return "Insufficient balance remaining to make the requested payment"
}

type quotaExceededError struct {
}

func NewQuotaExceededError() error {
	return &quotaExceededError{}
}

func (err *quotaExceededError) Error() string {
	return "Your app does not have enough budget remaining to make this payment. Please review this app in the connections page of your Alby Hub."
}

func NewTransactionsService(db *gorm.DB, eventPublisher events.EventPublisher) *transactionsService {
	return &transactionsService{
		db:             db,
		eventPublisher: eventPublisher,
	}
}

func (svc *transactionsService) MakeInvoice(ctx context.Context, amount uint64, description string, descriptionHash string, expiry uint64, metadata map[string]interface{}, lnClient lnclient.LNClient, appId *uint, requestEventId *uint) (*Transaction, error) {
	var metadataBytes []byte
	if metadata != nil {
		var err error
		metadataBytes, err = json.Marshal(metadata)
		if err != nil {
			logger.Logger.WithError(err).Error("Failed to serialize metadata")
			return nil, err
		}
		if len(metadataBytes) > constants.INVOICE_METADATA_MAX_LENGTH {
			return nil, fmt.Errorf("encoded invoice metadata provided is too large. Limit: %d Received: %d", constants.INVOICE_METADATA_MAX_LENGTH, len(metadataBytes))
		}
	}

	lnClientTransaction, err := lnClient.MakeInvoice(ctx, int64(amount), description, descriptionHash, int64(expiry))
	if err != nil {
		logger.Logger.WithError(err).Error("Failed to create transaction")
		return nil, err
	}

	var preimage *string
	if lnClientTransaction.Preimage != "" {
		preimage = &lnClientTransaction.Preimage
	}

	var expiresAt *time.Time
	if lnClientTransaction.ExpiresAt != nil {
		expiresAtValue := time.Unix(*lnClientTransaction.ExpiresAt, 0)
		expiresAt = &expiresAtValue
	}

	dbTransaction := db.Transaction{
		AppId:           appId,
		RequestEventId:  requestEventId,
		Type:            lnClientTransaction.Type,
		State:           constants.TRANSACTION_STATE_PENDING,
		AmountMsat:      uint64(lnClientTransaction.Amount),
		Description:     description,
		DescriptionHash: descriptionHash,
		PaymentRequest:  lnClientTransaction.Invoice,
		PaymentHash:     lnClientTransaction.PaymentHash,
		ExpiresAt:       expiresAt,
		Preimage:        preimage,
		Metadata:        datatypes.JSON(metadataBytes),
	}
	err = svc.db.Create(&dbTransaction).Error
	if err != nil {
		logger.Logger.WithError(err).Error("Failed to create DB transaction")
		return nil, err
	}
	return &dbTransaction, nil
}

func (svc *transactionsService) SendPaymentSync(ctx context.Context, payReq string, metadata map[string]interface{}, lnClient lnclient.LNClient, appId *uint, requestEventId *uint) (*Transaction, error) {
	var metadataBytes []byte
	if metadata != nil {
		var err error
		metadataBytes, err = json.Marshal(metadata)
		if err != nil {
			logger.Logger.WithError(err).Error("Failed to serialize metadata")
			return nil, err
		}
		if len(metadataBytes) > constants.INVOICE_METADATA_MAX_LENGTH {
			return nil, fmt.Errorf("encoded payment metadata provided is too large. Limit: %d Received: %d", constants.INVOICE_METADATA_MAX_LENGTH, len(metadataBytes))
		}
	}

	payReq = strings.ToLower(payReq)
	paymentRequest, err := decodepay.Decodepay(payReq)
	if err != nil {
		logger.Logger.WithFields(logrus.Fields{
			"bolt11": payReq,
		}).Errorf("Failed to decode bolt11 invoice: %v", err)

		return nil, err
	}

	selfPayment := paymentRequest.Payee != "" && paymentRequest.Payee == lnClient.GetPubkey()

	var dbTransaction db.Transaction

	err = svc.db.Transaction(func(tx *gorm.DB) error {
		var existingSettledTransaction db.Transaction
		if tx.Limit(1).Find(&existingSettledTransaction, &db.Transaction{
			Type:        constants.TRANSACTION_TYPE_OUTGOING,
			PaymentHash: paymentRequest.PaymentHash,
			State:       constants.TRANSACTION_STATE_SETTLED,
		}).RowsAffected > 0 {
			logger.Logger.WithField("payment_hash", dbTransaction.PaymentHash).Info("this invoice has already been paid")
			return errors.New("this invoice has already been paid")
		}

		err := svc.validateCanPay(tx, appId, uint64(paymentRequest.MSatoshi), paymentRequest.Description)
		if err != nil {
			return err
		}

		var expiresAt *time.Time
		if paymentRequest.Expiry > 0 {
			expiresAtValue := time.Now().Add(time.Duration(paymentRequest.Expiry) * time.Second)
			expiresAt = &expiresAtValue
		}
		dbTransaction = db.Transaction{
			AppId:           appId,
			RequestEventId:  requestEventId,
			Type:            constants.TRANSACTION_TYPE_OUTGOING,
			State:           constants.TRANSACTION_STATE_PENDING,
			FeeReserveMsat:  svc.calculateFeeReserveMsat(uint64(paymentRequest.MSatoshi)),
			AmountMsat:      uint64(paymentRequest.MSatoshi),
			PaymentRequest:  payReq,
			PaymentHash:     paymentRequest.PaymentHash,
			Description:     paymentRequest.Description,
			DescriptionHash: paymentRequest.DescriptionHash,
			ExpiresAt:       expiresAt,
			SelfPayment:     selfPayment,
			Metadata:        datatypes.JSON(metadataBytes),
		}
		err = tx.Create(&dbTransaction).Error
		return err
	})

	if err != nil {
		logger.Logger.WithFields(logrus.Fields{
			"bolt11": payReq,
		}).WithError(err).Error("Failed to create DB transaction")
		return nil, err
	}

	var response *lnclient.PayInvoiceResponse
	if selfPayment {
		response, err = svc.interceptSelfPayment(paymentRequest.PaymentHash)
	} else {
		response, err = lnClient.SendPaymentSync(ctx, payReq)
	}

	if err != nil {
		logger.Logger.WithFields(logrus.Fields{
			"bolt11": payReq,
		}).WithError(err).Error("Failed to send payment")

		if errors.Is(err, lnclient.NewTimeoutError()) {
			logger.Logger.WithFields(logrus.Fields{
				"bolt11": payReq,
			}).WithError(err).Error("Timed out waiting for payment to be sent. It may still succeed. Skipping update of transaction status")
			// we cannot update the payment to failed as it still might succeed.
			// we'll need to check the status of it later
			return nil, err
		}

		// As the LNClient did not return a timeout error, we assume the payment definitely failed
		svc.db.Transaction(func(tx *gorm.DB) error {
			return svc.markPaymentFailed(tx, &dbTransaction, err.Error())
		})

		return nil, err
	}

	// the payment definitely succeeded
	var settledTransaction *db.Transaction
	err = svc.db.Transaction(func(tx *gorm.DB) error {
		settledTransaction, err = svc.markTransactionSettled(tx, &dbTransaction, response.Preimage, response.Fee, selfPayment)
		return err
	})
	if err != nil {
		return nil, err
	}

	return settledTransaction, nil
}

func (svc *transactionsService) SendKeysend(ctx context.Context, amount uint64, destination string, customRecords []lnclient.TLVRecord, preimage string, lnClient lnclient.LNClient, appId *uint, requestEventId *uint) (*Transaction, error) {
	if preimage == "" {
		preImageBytes, err := makePreimageHex()
		if err != nil {
			return nil, err
		}
		preimage = hex.EncodeToString(preImageBytes)
	}

	preImageBytes, err := hex.DecodeString(preimage)
	if err != nil || len(preImageBytes) != 32 {
		logger.Logger.WithFields(logrus.Fields{
			"preimage": preimage,
		}).WithError(err).Error("Invalid preimage")
		return nil, err
	}

	paymentHash256 := sha256.New()
	paymentHash256.Write(preImageBytes)
	paymentHashBytes := paymentHash256.Sum(nil)
	paymentHash := hex.EncodeToString(paymentHashBytes)

	metadata := map[string]interface{}{}

	metadata["destination"] = destination

	metadata["tlv_records"] = customRecords
	metadataBytes, err := json.Marshal(metadata)
	if err != nil {
		logger.Logger.WithError(err).Error("Failed to serialize transaction metadata")
		return nil, err
	}
	boostagramBytes := svc.getBoostagramFromCustomRecords(customRecords)

	var dbTransaction db.Transaction

	selfPayment := destination == lnClient.GetPubkey()

	err = svc.db.Transaction(func(tx *gorm.DB) error {
		err := svc.validateCanPay(tx, appId, amount, "")
		if err != nil {
			return err
		}

		dbTransaction = db.Transaction{
			AppId:          appId,
			Description:    svc.getDescriptionFromCustomRecords(customRecords),
			RequestEventId: requestEventId,
			Type:           constants.TRANSACTION_TYPE_OUTGOING,
			State:          constants.TRANSACTION_STATE_PENDING,
			FeeReserveMsat: svc.calculateFeeReserveMsat(uint64(amount)),
			AmountMsat:     amount,
			Metadata:       datatypes.JSON(metadataBytes),
			Boostagram:     datatypes.JSON(boostagramBytes),
			PaymentHash:    paymentHash,
			Preimage:       &preimage,
			SelfPayment:    selfPayment,
		}
		err = tx.Create(&dbTransaction).Error

		return err
	})

	if err != nil {
		logger.Logger.WithFields(logrus.Fields{
			"destination": destination,
			"amount":      amount,
		}).WithError(err).Error("Failed to create DB transaction")
		return nil, err
	}

	var payKeysendResponse *lnclient.PayKeysendResponse

	if selfPayment {
		// for keysend self-payments we need to create an incoming payment at the time of the payment
		recipientAppId := svc.getAppIdFromCustomRecords(customRecords)
		dbTransaction := db.Transaction{
			AppId:          recipientAppId,
			RequestEventId: nil, // it is related to this request but for a different app
			Type:           constants.TRANSACTION_TYPE_INCOMING,
			State:          constants.TRANSACTION_STATE_PENDING,
			AmountMsat:     amount,
			PaymentHash:    paymentHash,
			Preimage:       &preimage,
			Description:    svc.getDescriptionFromCustomRecords(customRecords),
			Metadata:       datatypes.JSON(metadataBytes),
			Boostagram:     datatypes.JSON(boostagramBytes),
			SelfPayment:    true,
		}
		err = svc.db.Create(&dbTransaction).Error
		if err != nil {
			logger.Logger.WithError(err).Error("Failed to create DB transaction")
			return nil, err
		}

		_, err = svc.interceptSelfPayment(paymentHash)
		if err == nil {
			payKeysendResponse = &lnclient.PayKeysendResponse{
				Fee: 0,
			}
		}
	} else {
		payKeysendResponse, err = lnClient.SendKeysend(ctx, amount, destination, customRecords, preimage)
	}

	if err != nil {
		logger.Logger.WithFields(logrus.Fields{
			"destination": destination,
			"amount":      amount,
		}).WithError(err).Error("Failed to send payment")

		if errors.Is(err, lnclient.NewTimeoutError()) {

			logger.Logger.WithFields(logrus.Fields{
				"destination": destination,
				"amount":      amount,
			}).WithError(err).Error("Timed out waiting for payment to be sent. It may still succeed. Skipping update of transaction status")

			// we cannot update the payment to failed as it still might succeed.
			// we'll need to check the status of it later
			// but we have the payment hash now, so save it on the transaction
			dbErr := svc.db.Model(&dbTransaction).Updates(&db.Transaction{
				PaymentHash: paymentHash,
			}).Error
			if dbErr != nil {
				logger.Logger.WithFields(logrus.Fields{
					"destination": destination,
					"amount":      amount,
				}).WithError(dbErr).Error("Failed to update DB transaction")
			}
			return nil, err
		}

		// As the LNClient did not return a timeout error, we assume the payment definitely failed
		dbErr := svc.db.Model(&dbTransaction).Updates(&db.Transaction{
			PaymentHash: paymentHash,
			State:       constants.TRANSACTION_STATE_FAILED,
		}).Error
		if dbErr != nil {
			logger.Logger.WithFields(logrus.Fields{
				"destination": destination,
				"amount":      amount,
			}).WithError(dbErr).Error("Failed to update DB transaction")
		}

		return nil, err
	}

	// the payment definitely succeeded
	var settledTransaction *db.Transaction
	err = svc.db.Transaction(func(tx *gorm.DB) error {
		settledTransaction, err = svc.markTransactionSettled(tx, &dbTransaction, preimage, payKeysendResponse.Fee, selfPayment)
		return err
	})

	if err != nil {
		return nil, err
	}

	return settledTransaction, nil
}

func (svc *transactionsService) LookupTransaction(ctx context.Context, paymentHash string, transactionType *string, lnClient lnclient.LNClient, appId *uint) (*Transaction, error) {
	transaction := db.Transaction{}

	tx := svc.db

	if appId != nil {
		var app db.App
		result := svc.db.Limit(1).Find(&app, &db.App{
			ID: *appId,
		})
		if result.RowsAffected == 0 {
			return nil, NewNotFoundError()
		}
		if app.Isolated {
			tx = tx.Where("app_id == ?", *appId)
		}
	}

	if transactionType != nil {
		tx = tx.Where("type == ?", *transactionType)
	}

	// order settled first, otherwise by created date, as there can be multiple outgoing payments
	// for the same payment hash (if you tried to pay an invoice multiple times - e.g. the first time failed)
	result := tx.Order("settled_at desc, created_at desc").Limit(1).Find(&transaction, &db.Transaction{
		//Type:        transactionType,
		PaymentHash: paymentHash,
	})

	if result.Error != nil {
		logger.Logger.WithError(result.Error).Error("Failed to lookup transaction")
		return nil, result.Error
	}

	if result.RowsAffected == 0 {
		logger.Logger.WithFields(logrus.Fields{
			"payment_hash": paymentHash,
			"app_id":       appId,
		}).WithError(result.Error).Error("transaction not found")
		return nil, NewNotFoundError()
	}

	if transaction.State == constants.TRANSACTION_STATE_PENDING {
		svc.checkUnsettledTransaction(ctx, &transaction, lnClient)
	}

	return &transaction, nil
}

func (svc *transactionsService) ListTransactions(ctx context.Context, from, until, limit, offset uint64, unpaidOutgoing bool, unpaidIncoming bool, transactionType *string, lnClient lnclient.LNClient, appId *uint) (transactions []Transaction, err error) {
	svc.checkUnsettledTransactions(ctx, lnClient)

	tx := svc.db

	if !unpaidOutgoing && !unpaidIncoming {
		tx = tx.Where("state == ?", constants.TRANSACTION_STATE_SETTLED)
	} else if unpaidOutgoing && !unpaidIncoming {
		tx = tx.Where(tx.Where("state == ?", constants.TRANSACTION_STATE_SETTLED).
			Or("type == ?", constants.TRANSACTION_TYPE_OUTGOING))
	} else if unpaidIncoming && !unpaidOutgoing {
		tx = tx.Where(tx.Where("state == ?", constants.TRANSACTION_STATE_SETTLED).
			Or("type == ?", constants.TRANSACTION_TYPE_INCOMING))
	}

	if transactionType != nil {
		tx = tx.Where("type == ?", *transactionType)
	}

	if from > 0 {
		tx = tx.Where("created_at >= ?", time.Unix(int64(from), 0))
	}
	if until > 0 {
		tx = tx.Where("created_at <= ?", time.Unix(int64(until), 0))
	}

	if appId != nil {
		var app db.App
		result := svc.db.Limit(1).Find(&app, &db.App{
			ID: *appId,
		})
		if result.RowsAffected == 0 {
			return nil, NewNotFoundError()
		}
		if app.Isolated {
			tx = tx.Where("app_id == ?", *appId)
		}
	}

	tx = tx.Order("updated_at desc")

	if limit > 0 {
		tx = tx.Limit(int(limit))
	}
	if offset > 0 {
		tx = tx.Offset(int(offset))
	}

	result := tx.Find(&transactions)
	if result.Error != nil {
		logger.Logger.WithError(result.Error).Error("Failed to list DB transactions")
		return nil, result.Error
	}

	return transactions, nil
}

func (svc *transactionsService) checkUnsettledTransactions(ctx context.Context, lnClient lnclient.LNClient) {
	// Only check unsettled transactions for clients that don't support async events
	// checkUnsettledTransactions does not work for keysend payments!
	if slices.Contains(lnClient.GetSupportedNIP47NotificationTypes(), "payment_received") {
		return
	}

	// check pending payments less than a day old
	transactions := []Transaction{}
	result := svc.db.Where("state == ? AND created_at > ?", constants.TRANSACTION_STATE_PENDING, time.Now().Add(-24*time.Hour)).Find(&transactions)
	if result.Error != nil {
		logger.Logger.WithError(result.Error).Error("Failed to list DB transactions")
		return
	}
	for _, transaction := range transactions {
		svc.checkUnsettledTransaction(ctx, &transaction, lnClient)
	}
}
func (svc *transactionsService) checkUnsettledTransaction(ctx context.Context, transaction *db.Transaction, lnClient lnclient.LNClient) {
	if slices.Contains(lnClient.GetSupportedNIP47NotificationTypes(), "payment_received") {
		return
	}

	lnClientTransaction, err := lnClient.LookupInvoice(ctx, transaction.PaymentHash)
	if err != nil {
		logger.Logger.WithFields(logrus.Fields{
			"bolt11": transaction.PaymentRequest,
		}).WithError(err).Error("Failed to check transaction")
		return
	}
	// update transaction state
	if lnClientTransaction.SettledAt != nil {
		err = svc.db.Transaction(func(tx *gorm.DB) error {
			_, err = svc.markTransactionSettled(tx, transaction, lnClientTransaction.Preimage, uint64(lnClientTransaction.FeesPaid), false)
			return err
		})

		if err != nil {
			logger.Logger.WithError(err).Error("Failed to mark payment sent when checking unsettled transaction")
		}
	}
}

func (svc *transactionsService) ConsumeEvent(ctx context.Context, event *events.Event, globalProperties map[string]interface{}) {
	switch event.Event {
	case "nwc_lnclient_payment_received":
		lnClientTransaction, ok := event.Properties.(*lnclient.Transaction)
		if !ok {
			logger.Logger.WithField("event", event).Error("Failed to cast event")
			return
		}

		var dbTransaction db.Transaction
		err := svc.db.Transaction(func(tx *gorm.DB) error {

			result := tx.Limit(1).Find(&dbTransaction, &db.Transaction{
				Type:        constants.TRANSACTION_TYPE_INCOMING,
				PaymentHash: lnClientTransaction.PaymentHash,
			})

			if result.RowsAffected == 0 {
				var appId *uint
				description := lnClientTransaction.Description
				var metadataBytes []byte
				var boostagramBytes []byte
				if lnClientTransaction.Metadata != nil {
					var err error
					metadataBytes, err = json.Marshal(lnClientTransaction.Metadata)
					if err != nil {
						logger.Logger.WithError(err).Error("Failed to serialize transaction metadata")
						return err
					}

					var customRecords []lnclient.TLVRecord
					customRecords, _ = lnClientTransaction.Metadata["tlv_records"].([]lnclient.TLVRecord)
					boostagramBytes = svc.getBoostagramFromCustomRecords(customRecords)
					extractedDescription := svc.getDescriptionFromCustomRecords(customRecords)
					if extractedDescription != "" {
						description = extractedDescription
					}
					// find app by custom key/value records
					appId = svc.getAppIdFromCustomRecords(customRecords)
				}
				var expiresAt *time.Time
				if lnClientTransaction.ExpiresAt != nil {
					expiresAtValue := time.Unix(*lnClientTransaction.ExpiresAt, 0)
					expiresAt = &expiresAtValue
				}
				dbTransaction = db.Transaction{
					Type:            constants.TRANSACTION_TYPE_INCOMING,
					AmountMsat:      uint64(lnClientTransaction.Amount),
					PaymentRequest:  lnClientTransaction.Invoice,
					PaymentHash:     lnClientTransaction.PaymentHash,
					Description:     description,
					DescriptionHash: lnClientTransaction.DescriptionHash,
					ExpiresAt:       expiresAt,
					Metadata:        datatypes.JSON(metadataBytes),
					Boostagram:      datatypes.JSON(boostagramBytes),
					AppId:           appId,
				}
				err := tx.Create(&dbTransaction).Error
				if err != nil {
					logger.Logger.WithFields(logrus.Fields{
						"payment_hash": lnClientTransaction.PaymentHash,
					}).WithError(err).Error("Failed to create transaction")
					return err
				}
			}

			_, err := svc.markTransactionSettled(tx, &dbTransaction, lnClientTransaction.Preimage, uint64(lnClientTransaction.FeesPaid), false)
			return err
		})

		if err != nil {
			logger.Logger.WithFields(logrus.Fields{
				"payment_hash": lnClientTransaction.PaymentHash,
			}).WithError(err).Error("Failed to execute DB transaction")
			return
		}
	case "nwc_lnclient_payment_sent":
		lnClientTransaction, ok := event.Properties.(*lnclient.Transaction)
		if !ok {
			logger.Logger.WithField("event", event).Error("Failed to cast event")
			return
		}

		var dbTransaction db.Transaction
		err := svc.db.Transaction(func(tx *gorm.DB) error {
			result := tx.Limit(1).Find(&dbTransaction, &db.Transaction{
				Type:        constants.TRANSACTION_TYPE_OUTGOING,
				PaymentHash: lnClientTransaction.PaymentHash,
			})

			if result.Error != nil {
				return result.Error
			}

			if result.RowsAffected == 0 {
				// Note: payments made from outside cannot be associated with an app
				// for now this is disabled as it only applies to LND, and we do not import LND transactions either.
				logger.Logger.WithField("payment_hash", lnClientTransaction.PaymentHash).Error("payment not found")
				return NewNotFoundError()
			}

			_, err := svc.markTransactionSettled(tx, &dbTransaction, lnClientTransaction.Preimage, uint64(lnClientTransaction.FeesPaid), false)
			return err
		})

		if err != nil {
			logger.Logger.WithFields(logrus.Fields{
				"payment_hash": lnClientTransaction.PaymentHash,
			}).WithError(err).Error("Failed to update transaction")
			return
		}
	case "nwc_lnclient_payment_failed":
		paymentFailedAsyncProperties, ok := event.Properties.(*lnclient.PaymentFailedEventProperties)
		if !ok {
			logger.Logger.WithField("event", event).Error("Failed to cast event")
			return
		}

		lnClientTransaction := paymentFailedAsyncProperties.Transaction

		var dbTransaction db.Transaction
		result := svc.db.Limit(1).Find(&dbTransaction, &db.Transaction{
			Type:        constants.TRANSACTION_TYPE_OUTGOING,
			PaymentHash: lnClientTransaction.PaymentHash,
		})

		if result.RowsAffected == 0 {
			logger.Logger.WithField("event", event).Error("Failed to find outgoing transaction by payment hash")
			return
		}

		svc.db.Transaction(func(tx *gorm.DB) error {
			return svc.markPaymentFailed(tx, &dbTransaction, paymentFailedAsyncProperties.Reason)
		})
	}
}

func (svc *transactionsService) interceptSelfPayment(paymentHash string) (*lnclient.PayInvoiceResponse, error) {
	logger.Logger.WithField("payment_hash", paymentHash).Debug("Intercepting self payment")
	incomingTransaction := db.Transaction{}
	result := svc.db.Limit(1).Find(&incomingTransaction, &db.Transaction{
		Type:        constants.TRANSACTION_TYPE_INCOMING,
		State:       constants.TRANSACTION_STATE_PENDING,
		PaymentHash: paymentHash,
	})
	if result.Error != nil {
		return nil, result.Error
	}

	if result.RowsAffected == 0 {
		return nil, NewNotFoundError()
	}
	if incomingTransaction.Preimage == nil {
		return nil, errors.New("preimage is not set on transaction. Self payments not supported")
	}

	err := svc.db.Transaction(func(tx *gorm.DB) error {
		_, err := svc.markTransactionSettled(tx, &incomingTransaction, *incomingTransaction.Preimage, uint64(0), true)
		return err
	})

	if err != nil {
		return nil, err
	}

	return &lnclient.PayInvoiceResponse{
		Preimage: *incomingTransaction.Preimage,
		Fee:      0,
	}, nil
}

func (svc *transactionsService) validateCanPay(tx *gorm.DB, appId *uint, amount uint64, description string) error {
	amountWithFeeReserve := amount + svc.calculateFeeReserveMsat(amount)

	// ensure balance for isolated apps
	if appId != nil {
		var app db.App
		result := tx.Limit(1).Find(&app, &db.App{
			ID: *appId,
		})
		if result.RowsAffected == 0 {
			return NewNotFoundError()
		}

		var appPermission db.AppPermission
		result = tx.Limit(1).Find(&appPermission, &db.AppPermission{
			AppId: *appId,
			Scope: constants.PAY_INVOICE_SCOPE,
		})
		if result.RowsAffected == 0 {
			return errors.New("app does not have pay_invoice scope")
		}

		if app.Isolated {
			balance := queries.GetIsolatedBalance(tx, appPermission.AppId)

			if amountWithFeeReserve > balance {
				message := NewInsufficientBalanceError().Error()
				if description != "" {
					message += " " + description
				}

				svc.eventPublisher.Publish(&events.Event{
					Event: "nwc_permission_denied",
					Properties: map[string]interface{}{
						"app_name": app.Name,
						"code":     constants.ERROR_INSUFFICIENT_BALANCE,
						"message":  message,
					},
				})
				return NewInsufficientBalanceError()
			}
		}

		if appPermission.MaxAmountSat > 0 {
			budgetUsageSat := queries.GetBudgetUsageSat(tx, &appPermission)
			if int(amountWithFeeReserve/1000) > appPermission.MaxAmountSat-int(budgetUsageSat) {
				message := NewQuotaExceededError().Error()
				if description != "" {
					message += " " + description
				}
				svc.eventPublisher.Publish(&events.Event{
					Event: "nwc_permission_denied",
					Properties: map[string]interface{}{
						"app_name": app.Name,
						"code":     constants.ERROR_QUOTA_EXCEEDED,
						"message":  message,
					},
				})
				return NewQuotaExceededError()
			}
		}
	}

	return nil
}

// max of 1% or 10000 millisats (10 sats)
func (svc *transactionsService) calculateFeeReserveMsat(amount uint64) uint64 {
	// NOTE: LDK defaults to 1% of the payment amount + 50 sats
	return uint64(math.Max(math.Ceil(float64(amount)*0.01), 10000))
}

func makePreimageHex() ([]byte, error) {
	bytes := make([]byte, 32) // 32 bytes * 8 bits/byte = 256 bits
	_, err := rand.Read(bytes)
	if err != nil {
		return nil, err
	}
	return bytes, nil
}

func (svc *transactionsService) getBoostagramFromCustomRecords(customRecords []lnclient.TLVRecord) []byte {
	for _, record := range customRecords {
		if record.Type == BoostagramTlvType {
			bytes, err := hex.DecodeString(record.Value)
			if err != nil {
				return nil
			}
			return bytes
		}
	}

	return nil
}

func (svc *transactionsService) getDescriptionFromCustomRecords(customRecords []lnclient.TLVRecord) string {
	var description string

	for _, record := range customRecords {
		switch record.Type {
		case BoostagramTlvType:
			bytes, err := hex.DecodeString(record.Value)
			if err != nil {
				continue
			}
			var boostagram Boostagram
			if err := json.Unmarshal(bytes, &boostagram); err != nil {
				continue
			}
			return boostagram.Message

		// TODO: consider adding support for this in LDK
		case WhatsatTlvType:
			bytes, err := hex.DecodeString(record.Value)
			if err == nil {
				description = string(bytes)
			}
		}
	}

	return description
}

func (svc *transactionsService) getAppIdFromCustomRecords(customRecords []lnclient.TLVRecord) *uint {
	app := db.App{}
	for _, record := range customRecords {
		if record.Type == CustomKeyTlvType {
			decodedString, err := hex.DecodeString(record.Value)
			if err != nil {
				logger.Logger.WithError(err).Error("Failed to parse custom key TLV record as hex")
				continue
			}
			customValue, err := strconv.ParseUint(string(decodedString), 10, 64)
			if err != nil {
				logger.Logger.WithError(err).Error("Failed to parse custom key TLV record as number")
				continue
			}
			err = svc.db.Take(&app, &db.App{
				ID: uint(customValue),
			}).Error
			if err != nil {
				logger.Logger.WithError(err).Error("Failed to find app by id from custom key TLV record")
				continue
			}
			return &app.ID
		}
	}
	return nil
}

func (svc *transactionsService) markTransactionSettled(tx *gorm.DB, dbTransaction *db.Transaction, preimage string, fee uint64, selfPayment bool) (*db.Transaction, error) {
	// TODO: it would be better to have a database constraint so we cannot have two pending payments
	var existingSettledTransaction db.Transaction
	if tx.Limit(1).Find(&existingSettledTransaction, &db.Transaction{
		Type:        dbTransaction.Type,
		PaymentHash: dbTransaction.PaymentHash,
		State:       constants.TRANSACTION_STATE_SETTLED,
	}).RowsAffected > 0 {
		logger.Logger.WithField("payment_hash", dbTransaction.PaymentHash).Error("payment already marked as sent")
		return &existingSettledTransaction, nil
	}

	if preimage == "" {
		return nil, errors.New("no preimage in payment")
	}

	now := time.Now()
	err := tx.Model(dbTransaction).Updates(map[string]interface{}{
		"State":          constants.TRANSACTION_STATE_SETTLED,
		"Preimage":       &preimage,
		"FeeMsat":        fee,
		"FeeReserveMsat": 0,
		"SettledAt":      &now,
		"SelfPayment":    selfPayment,
	}).Error
	if err != nil {
		logger.Logger.WithFields(logrus.Fields{
			"payment_hash": dbTransaction.PaymentHash,
		}).WithError(err).Error("Failed to update DB transaction")
		return nil, err
	}

	logger.Logger.WithFields(logrus.Fields{
		"payment_hash": dbTransaction.PaymentHash,
		"type":         dbTransaction.Type,
	}).Info("Marked transaction as settled")

	event := "nwc_payment_sent"
	if dbTransaction.Type == constants.TRANSACTION_TYPE_INCOMING {
		event = "nwc_payment_received"
	}

	svc.eventPublisher.Publish(&events.Event{
		Event:      event,
		Properties: dbTransaction,
	})

	return dbTransaction, nil
}

func (svc *transactionsService) markPaymentFailed(tx *gorm.DB, dbTransaction *db.Transaction, reason string) error {
	var existingTransaction db.Transaction
	result := tx.Limit(1).Find(&existingTransaction, &db.Transaction{
		ID: dbTransaction.ID,
	})

	if result.Error != nil {
		logger.Logger.WithField("payment_hash", dbTransaction.PaymentHash).WithError(result.Error).Error("could not find transaction to mark as failed")
		return result.Error
	}

	if existingTransaction.State == constants.TRANSACTION_STATE_FAILED {
		logger.Logger.WithField("payment_hash", dbTransaction.PaymentHash).Info("payment already marked as failed")
		return nil
	}

	err := tx.Model(dbTransaction).Updates(map[string]interface{}{
		"State":          constants.TRANSACTION_STATE_FAILED,
		"FeeReserveMsat": 0,
		"FailureReason":  reason,
	}).Error
	if err != nil {
		logger.Logger.WithFields(logrus.Fields{
			"payment_hash": dbTransaction.PaymentHash,
		}).WithError(err).Error("Failed to mark transaction as failed")
		return err
	}
	logger.Logger.WithField("payment_hash", dbTransaction.PaymentHash).Info("Marked transaction as failed")

	svc.eventPublisher.Publish(&events.Event{
		Event:      "nwc_payment_failed",
		Properties: dbTransaction,
	})
	return nil
}
