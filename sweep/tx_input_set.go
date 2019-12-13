package sweep

import (
	"github.com/decred/dcrd/dcrutil/v2"
	"github.com/decred/dcrlnd/input"
	"github.com/decred/dcrlnd/lnwallet"
	"github.com/decred/dcrlnd/lnwallet/chainfee"
)

// txInputSet is an object that accumulates tx inputs and keeps running counters
// on various properties of the tx.
type txInputSet struct {
	// sizeEstimate is the (worst case) tx weight with the current set of
	// inputs.
	sizeEstimate input.TxSizeEstimator

	// inputTotal is the total value of all inputs.
	inputTotal dcrutil.Amount

	// outputValue is the value of the tx output.
	outputValue dcrutil.Amount

	// feePerKB is the fee rate used to calculate the tx fee.
	feePerKB chainfee.AtomPerKByte

	// inputs is the set of tx inputs.
	inputs []input.Input

	// dustLimit is the minimum output value of the tx.
	dustLimit dcrutil.Amount

	// maxInputs is the maximum number of inputs that will be accepted in
	// the set.
	maxInputs int
}

// newTxInputSet constructs a new, empty input set.
func newTxInputSet(feePerKB, relayFee chainfee.AtomPerKByte,
	maxInputs int) *txInputSet {

	// Calculate dust limit based on the P2PKH output script of the sweep
	// txes.
	dustLimit := lnwallet.DustThresholdForRelayFee(relayFee)

	b := txInputSet{
		feePerKB:  feePerKB,
		dustLimit: dustLimit,
		maxInputs: maxInputs,
	}

	// Add the sweep tx output to the size estimate.
	b.sizeEstimate.AddP2PKHOutput()

	return &b
}

// dustLimitReached returns true if we've accumulated enough inputs to meet the
// dust limit.
func (t *txInputSet) dustLimitReached() bool {
	return t.outputValue >= t.dustLimit
}

// add adds a new input to the set. It returns a bool indicating whether the
// input was added to the set. An input is rejected if it decreases the tx
// output value after paying fees.
func (t *txInputSet) add(input input.Input) bool {
	// Stop if max inputs is reached.
	if len(t.inputs) == t.maxInputs {
		return false
	}

	// Can ignore error, because it has already been checked when
	// calculating the yields.
	size, isNestedP2SH, _ := input.WitnessType().SizeUpperBound()

	// Add size of this new candidate input to a copy of the weight
	// estimator.
	newSizeEstimate := t.sizeEstimate
	if isNestedP2SH {
		// This should never happen in decred.
		log.Errorf("Attempting to sweep nested P2SH %v", input.OutPoint())
		return false
	} else {
		newSizeEstimate.AddCustomInput(size)
	}

	value := dcrutil.Amount(input.SignDesc().Output.Value)
	newInputTotal := t.inputTotal + value

	size = newSizeEstimate.Size()
	fee := t.feePerKB.FeeForSize(int64(size))

	// Calculate the output value if the current input would be
	// added to the set.
	newOutputValue := newInputTotal - fee

	// If adding this input makes the total output value of the set
	// decrease, this is a negative yield input. We don't add the input to
	// the set and return the outcome.
	if newOutputValue <= t.outputValue {
		return false
	}

	// Update running values.
	t.inputTotal = newInputTotal
	t.outputValue = newOutputValue
	t.inputs = append(t.inputs, input)
	t.sizeEstimate = newSizeEstimate

	return true
}

// addPositiveYieldInputs adds sweepableInputs that have a positive yield to the
// input set. This function assumes that the list of inputs is sorted descending
// by yield.
//
// TODO(roasbeef): Consider including some negative yield inputs too to clean
// up the utxo set even if it costs us some fees up front.  In the spirit of
// minimizing any negative externalities we cause for the Bitcoin system as a
// whole.
func (t *txInputSet) addPositiveYieldInputs(sweepableInputs []txInput) {
	for _, input := range sweepableInputs {
		// Try to add the input to the transaction. If that doesn't
		// succeed because it wouldn't increase the output value,
		// return. Assuming inputs are sorted by yield, any further
		// inputs wouldn't increase the output value either.
		if !t.add(input) {
			return
		}
	}

	// We managed to add all inputs to the set.
}
