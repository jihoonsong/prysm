package peerdas

import (
	"encoding/binary"
	"math"

	cKzg4844 "github.com/ethereum/c-kzg-4844/bindings/go"
	"github.com/ethereum/go-ethereum/p2p/enode"
	"github.com/holiman/uint256"
	errors "github.com/pkg/errors"
	fieldparams "github.com/prysmaticlabs/prysm/v5/config/fieldparams"
	"github.com/prysmaticlabs/prysm/v5/config/params"
	"github.com/prysmaticlabs/prysm/v5/consensus-types/blocks"
	"github.com/prysmaticlabs/prysm/v5/consensus-types/interfaces"
	"github.com/prysmaticlabs/prysm/v5/crypto/hash"
	"github.com/prysmaticlabs/prysm/v5/encoding/bytesutil"
	ethpb "github.com/prysmaticlabs/prysm/v5/proto/prysm/v1alpha1"
)

const (
	// Bytes per cell
	bytesPerCell = cKzg4844.FieldElementsPerCell * cKzg4844.BytesPerFieldElement

	// Number of cells in the extended matrix
	extendedMatrixSize = fieldparams.MaxBlobsPerBlock * cKzg4844.CellsPerExtBlob
)

type (
	ExtendedMatrix []cKzg4844.Cell

	cellCoordinate struct {
		blobIndex uint64
		cellID    uint64
	}
)

var (
	// Custom errors
	errCustodySubnetCountTooLarge = errors.New("custody subnet count larger than data column sidecar subnet count")
	errCellNotFound               = errors.New("cell not found (should never happen)")
	errIndexTooLarge              = errors.New("column index is larger than the specified number of columns")
	errMismatchLength             = errors.New("mismatch in the length of the commitments and proofs")

	// maxUint256 is the maximum value of a uint256.
	maxUint256 = &uint256.Int{math.MaxUint64, math.MaxUint64, math.MaxUint64, math.MaxUint64}
)

// CustodyColumns computes the columns the node should custody.
// https://github.com/ethereum/consensus-specs/blob/dev/specs/_features/eip7594/das-core.md#helper-functions
func CustodyColumns(nodeId enode.ID, custodySubnetCount uint64) (map[uint64]bool, error) {
	dataColumnSidecarSubnetCount := params.BeaconConfig().DataColumnSidecarSubnetCount

	// Compute the custodied subnets.
	subnetIds, err := CustodyColumnSubnets(nodeId, custodySubnetCount)
	if err != nil {
		return nil, errors.Wrap(err, "custody subnets")
	}

	columnsPerSubnet := cKzg4844.CellsPerExtBlob / dataColumnSidecarSubnetCount

	// Knowing the subnet ID and the number of columns per subnet, select all the columns the node should custody.
	// Columns belonging to the same subnet are contiguous.
	columnIndices := make(map[uint64]bool, custodySubnetCount*columnsPerSubnet)
	for i := uint64(0); i < columnsPerSubnet; i++ {
		for subnetId := range subnetIds {
			columnIndex := dataColumnSidecarSubnetCount*i + subnetId
			columnIndices[columnIndex] = true
		}
	}

	return columnIndices, nil
}

func CustodyColumnSubnets(nodeId enode.ID, custodySubnetCount uint64) (map[uint64]bool, error) {
	dataColumnSidecarSubnetCount := params.BeaconConfig().DataColumnSidecarSubnetCount

	// Check if the custody subnet count is larger than the data column sidecar subnet count.
	if custodySubnetCount > dataColumnSidecarSubnetCount {
		return nil, errCustodySubnetCountTooLarge
	}

	// First, compute the subnet IDs that the node should participate in.
	subnetIds := make(map[uint64]bool, custodySubnetCount)

	one := uint256.NewInt(1)

	for currentId := new(uint256.Int).SetBytes(nodeId.Bytes()); uint64(len(subnetIds)) < custodySubnetCount; currentId.Add(currentId, one) {
		// Convert to big endian bytes.
		currentIdBytesBigEndian := currentId.Bytes32()

		// Convert to little endian.
		currentIdBytesLittleEndian := bytesutil.ReverseByteOrder(currentIdBytesBigEndian[:])

		// Hash the result.
		hashedCurrentId := hash.Hash(currentIdBytesLittleEndian)

		// Get the subnet ID.
		subnetId := binary.LittleEndian.Uint64(hashedCurrentId[:8]) % dataColumnSidecarSubnetCount

		// Add the subnet to the map.
		subnetIds[subnetId] = true

		// Overflow prevention.
		if currentId.Cmp(maxUint256) == 0 {
			currentId = uint256.NewInt(0)
		}
	}

	return subnetIds, nil
}

// ComputeExtendedMatrix computes the extended matrix from the blobs.
// https://github.com/ethereum/consensus-specs/blob/dev/specs/_features/eip7594/das-core.md#compute_extended_matrix
func ComputeExtendedMatrix(blobs []cKzg4844.Blob) (ExtendedMatrix, error) {
	matrix := make(ExtendedMatrix, 0, extendedMatrixSize)

	for i := range blobs {
		// Chunk a non-extended blob into cells representing the corresponding extended blob.
		blob := &blobs[i]
		cells, err := cKzg4844.ComputeCells(blob)
		if err != nil {
			return nil, errors.Wrap(err, "compute cells for blob")
		}

		matrix = append(matrix, cells[:]...)
	}

	return matrix, nil
}

// RecoverMatrix recovers the extended matrix from some cells.
// https://github.com/ethereum/consensus-specs/blob/dev/specs/_features/eip7594/das-core.md#recover_matrix
func RecoverMatrix(cellFromCoordinate map[cellCoordinate]cKzg4844.Cell, blobCount uint64) (ExtendedMatrix, error) {
	matrix := make(ExtendedMatrix, 0, extendedMatrixSize)

	for blobIndex := uint64(0); blobIndex < blobCount; blobIndex++ {
		// Filter all cells that belong to the current blob.
		cellIds := make([]uint64, 0, cKzg4844.CellsPerExtBlob)
		for coordinate := range cellFromCoordinate {
			if coordinate.blobIndex == blobIndex {
				cellIds = append(cellIds, coordinate.cellID)
			}
		}

		// Retrieve cells corresponding to all `cellIds`.
		cellIdsCount := len(cellIds)

		cells := make([]cKzg4844.Cell, 0, cellIdsCount)
		for _, cellId := range cellIds {
			coordinate := cellCoordinate{blobIndex: blobIndex, cellID: cellId}
			cell, ok := cellFromCoordinate[coordinate]
			if !ok {
				return matrix, errCellNotFound
			}

			cells = append(cells, cell)
		}

		// Recover all cells.
		allCellsForRow, err := cKzg4844.RecoverAllCells(cellIds, cells)
		if err != nil {
			return matrix, errors.Wrap(err, "recover all cells")
		}

		matrix = append(matrix, allCellsForRow[:]...)
	}

	return matrix, nil
}

// DataColumnSidecars computes the data column sidecars from the signed block and blobs.
// https://github.com/ethereum/consensus-specs/blob/dev/specs/_features/eip7594/das-core.md#recover_matrix
func DataColumnSidecars(signedBlock interfaces.SignedBeaconBlock, blobs []cKzg4844.Blob) ([]*ethpb.DataColumnSidecar, error) {
	blobsCount := len(blobs)
	if blobsCount == 0 {
		return nil, nil
	}

	// Get the signed block header.
	signedBlockHeader, err := signedBlock.Header()
	if err != nil {
		return nil, errors.Wrap(err, "signed block header")
	}

	// Get the block body.
	block := signedBlock.Block()
	blockBody := block.Body()

	// Get the blob KZG commitments.
	blobKzgCommitments, err := blockBody.BlobKzgCommitments()
	if err != nil {
		return nil, errors.Wrap(err, "blob KZG commitments")
	}

	// Compute the KZG commitments inclusion proof.
	kzgCommitmentsInclusionProof, err := blocks.MerkleProofKZGCommitments(blockBody)
	if err != nil {
		return nil, errors.Wrap(err, "merkle proof ZKG commitments")
	}

	// Compute cells and proofs.
	cells := make([][cKzg4844.CellsPerExtBlob]cKzg4844.Cell, 0, blobsCount)
	proofs := make([][cKzg4844.CellsPerExtBlob]cKzg4844.KZGProof, 0, blobsCount)

	for i := range blobs {
		blob := &blobs[i]
		blobCells, blobProofs, err := cKzg4844.ComputeCellsAndKZGProofs(blob)
		if err != nil {
			return nil, errors.Wrap(err, "compute cells and KZG proofs")
		}

		cells = append(cells, blobCells)
		proofs = append(proofs, blobProofs)
	}

	// Get the column sidecars.
	sidecars := make([]*ethpb.DataColumnSidecar, 0, cKzg4844.CellsPerExtBlob)
	for columnIndex := uint64(0); columnIndex < cKzg4844.CellsPerExtBlob; columnIndex++ {
		column := make([]cKzg4844.Cell, 0, blobsCount)
		kzgProofOfColumn := make([]cKzg4844.KZGProof, 0, blobsCount)

		for rowIndex := 0; rowIndex < blobsCount; rowIndex++ {
			cell := cells[rowIndex][columnIndex]
			column = append(column, cell)

			kzgProof := proofs[rowIndex][columnIndex]
			kzgProofOfColumn = append(kzgProofOfColumn, kzgProof)
		}

		columnBytes := make([][]byte, 0, blobsCount)
		for i := range column {
			cell := column[i]

			cellBytes := make([]byte, 0, bytesPerCell)
			for _, fieldElement := range cell {
				copiedElem := fieldElement
				cellBytes = append(cellBytes, copiedElem[:]...)
			}

			columnBytes = append(columnBytes, cellBytes)
		}

		kzgProofOfColumnBytes := make([][]byte, 0, blobsCount)
		for _, kzgProof := range kzgProofOfColumn {
			copiedProof := kzgProof
			kzgProofOfColumnBytes = append(kzgProofOfColumnBytes, copiedProof[:])
		}

		sidecar := &ethpb.DataColumnSidecar{
			ColumnIndex:                  columnIndex,
			DataColumn:                   columnBytes,
			KzgCommitments:               blobKzgCommitments,
			KzgProof:                     kzgProofOfColumnBytes,
			SignedBlockHeader:            signedBlockHeader,
			KzgCommitmentsInclusionProof: kzgCommitmentsInclusionProof,
		}

		sidecars = append(sidecars, sidecar)
	}

	return sidecars, nil
}

// VerifyDataColumnSidecarKZGProofs verifies the provided KZG Proofs for the particular
// data column.
func VerifyDataColumnSidecarKZGProofs(sc *ethpb.DataColumnSidecar) (bool, error) {
	if sc.ColumnIndex >= params.BeaconConfig().NumberOfColumns {
		return false, errIndexTooLarge
	}
	if len(sc.DataColumn) != len(sc.KzgCommitments) || len(sc.KzgCommitments) != len(sc.KzgProof) {
		return false, errMismatchLength
	}
	blobsCount := len(sc.DataColumn)

	rowIdx := make([]uint64, 0, blobsCount)
	colIdx := make([]uint64, 0, blobsCount)
	for i := 0; i < len(sc.DataColumn); i++ {
		copiedI := uint64(i)
		rowIdx = append(rowIdx, copiedI)
		colI := sc.ColumnIndex
		colIdx = append(colIdx, colI)
	}
	ckzgComms := make([]cKzg4844.Bytes48, 0, len(sc.KzgCommitments))
	for _, com := range sc.KzgCommitments {
		ckzgComms = append(ckzgComms, cKzg4844.Bytes48(com))
	}
	var cells []cKzg4844.Cell
	for _, ce := range sc.DataColumn {
		var newCell []cKzg4844.Bytes32
		for i := 0; i < len(ce); i += 32 {
			newCell = append(newCell, cKzg4844.Bytes32(ce[i:i+32]))
		}
		cells = append(cells, cKzg4844.Cell(newCell))
	}
	var proofs []cKzg4844.Bytes48
	for _, p := range sc.KzgProof {
		proofs = append(proofs, cKzg4844.Bytes48(p))
	}
	return cKzg4844.VerifyCellKZGProofBatch(ckzgComms, rowIdx, colIdx, cells, proofs)
}