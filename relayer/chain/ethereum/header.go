// Copyright 2020 Snowfork
// SPDX-License-Identifier: LGPL-3.0-only

package ethereum

import (
	"fmt"
	"math/big"

	types "github.com/centrifuge/go-substrate-rpc-client/types"
	etypes "github.com/ethereum/go-ethereum/core/types"
	"github.com/ethereum/go-ethereum/rlp"
	"github.com/sirupsen/logrus"
	"github.com/snowfork/polkadot-ethereum/relayer/chain"
	"github.com/tranvictor/ethashproof"
	"github.com/tranvictor/ethashproof/ethash"
)

type HeaderID struct {
	Number types.U64
	Hash   types.H256
}

type Header struct {
	ParentHash       types.H256
	Timestamp        types.U64
	Number           types.U64
	Author           types.H160
	TransactionsRoot types.H256
	OmmersHash       types.H256
	ExtraData        types.Bytes
	StateRoot        types.H256
	ReceiptsRoot     types.H256
	LogsBloom        types.Bytes256
	GasUsed          types.U256
	GasLimit         types.U256
	Difficulty       types.U256
	Seal             []types.Bytes
}

type DoubleNodeWithMerkleProof struct {
	DagNodes [2]types.H512
	Proof    [][16]byte
}

func MakeHeaderFromEthHeader(gethheader *etypes.Header, proofcache *ethashproof.DatasetMerkleTreeCache, log *logrus.Entry) (*chain.Header, error) {

	// Convert Geth types to their Substrate Go client counterparts that match our node
	var blockNumber uint64
	if !gethheader.Number.IsUint64() {
		return nil, fmt.Errorf("gethheader.Number is not uint64")
	}
	blockNumber = gethheader.Number.Uint64()
	if !gethheader.Time.IsUint64() {
		return nil, fmt.Errorf("gethheader.Time is not uint64")
	}

	var gasUsed, gasLimit big.Int
	gasUsed.SetUint64(gethheader.GasUsed)
	gasLimit.SetUint64(gethheader.GasLimit)

	var bloomBytes [256]byte
	copy(bloomBytes[:], gethheader.Bloom.Bytes())

	mixHashRLP, err := rlp.EncodeToBytes(gethheader.MixDigest)
	if err != nil {
		return nil, err
	}

	nonceRLP, err := rlp.EncodeToBytes(gethheader.Nonce)
	if err != nil {
		return nil, err
	}

	header := Header{
		ParentHash:       types.NewH256(gethheader.ParentHash.Bytes()),
		Timestamp:        types.NewU64(gethheader.Time.Uint64()),
		Number:           types.NewU64(blockNumber),
		Author:           types.NewH160(gethheader.Coinbase.Bytes()),
		TransactionsRoot: types.NewH256(gethheader.TxHash.Bytes()),
		OmmersHash:       types.NewH256(gethheader.UncleHash.Bytes()),
		ExtraData:        types.NewBytes(gethheader.Extra),
		StateRoot:        types.NewH256(gethheader.Root.Bytes()),
		ReceiptsRoot:     types.NewH256(gethheader.ReceiptHash.Bytes()),
		LogsBloom:        types.NewBytes256(bloomBytes),
		GasUsed:          types.NewU256(gasUsed),
		GasLimit:         types.NewU256(gasLimit),
		Difficulty:       types.NewU256(*gethheader.Difficulty),
		Seal:             []types.Bytes{mixHashRLP, nonceRLP},
	}

	// Generate merkle proofs for Ethash
	indices := ethash.Instance.GetVerificationIndices(
		blockNumber,
		ethash.Instance.SealHash(gethheader),
		gethheader.Nonce.Uint64(),
	)

	proofData := make([]DoubleNodeWithMerkleProof, len(indices))
	for i, index := range indices {
		element, proof, err := ethashproof.CalculateProof(blockNumber, index, proofcache)
		if err != nil {
			return nil, err
		}

		es := element.ToUint256Array()
		node1Bytes := make([]byte, 64)
		node2Bytes := make([]byte, 64)
		// Each 32 byte sequence is left-padded with 0
		copy(node1Bytes[32-len(es[0].Bytes()):32], es[0].Bytes())
		copy(node1Bytes[64-len(es[1].Bytes()):], es[1].Bytes())
		copy(node2Bytes[32-len(es[2].Bytes()):32], es[2].Bytes())
		copy(node2Bytes[64-len(es[3].Bytes()):], es[3].Bytes())
		proofH128 := make([][16]byte, len(proof))
		for j, pr := range proof {
			proofH128[j] = [16]byte(pr)
		}

		proofData[i] = DoubleNodeWithMerkleProof{
			DagNodes: [2]types.H512{
				types.NewH512(node1Bytes),
				types.NewH512(node2Bytes),
			},
			Proof: proofH128,
		}
	}

	log.WithFields(logrus.Fields{
		"blockNumber": gethheader.Number,
	}).Debug("Generated header from Ethereum header")

	return &chain.Header{HeaderData: header, ProofData: proofData}, nil
}
