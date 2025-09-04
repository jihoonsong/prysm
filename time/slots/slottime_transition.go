package slots

import (
	"github.com/OffchainLabs/prysm/v6/config/params"
	"github.com/OffchainLabs/prysm/v6/consensus-types/primitives"
)

func PostEip7782(slot primitives.Slot) bool {
	return slot >= params.BeaconConfig().SlotsPerEpoch.Mul(uint64(params.BeaconConfig().Eip7782ForkEpoch))
}
