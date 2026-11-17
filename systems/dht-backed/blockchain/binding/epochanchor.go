// Code generated - DO NOT EDIT.
// This file is a generated binding and any manual changes will be lost.

package binding

import (
	"errors"
	"math/big"
	"strings"

	ethereum "github.com/ethereum/go-ethereum"
	"github.com/ethereum/go-ethereum/accounts/abi"
	"github.com/ethereum/go-ethereum/accounts/abi/bind"
	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/core/types"
	"github.com/ethereum/go-ethereum/event"
)

// Reference imports to suppress errors if they are not otherwise used.
var (
	_ = errors.New
	_ = big.NewInt
	_ = strings.NewReader
	_ = ethereum.NotFound
	_ = bind.Bind
	_ = common.Big1
	_ = types.BloomLookup
	_ = event.NewSubscription
	_ = abi.ConvertType
)

// EpochAnchorMetaData contains all meta data concerning the EpochAnchor contract.
var EpochAnchorMetaData = &bind.MetaData{
	ABI: "[{\"inputs\":[],\"stateMutability\":\"nonpayable\",\"type\":\"constructor\"},{\"anonymous\":false,\"inputs\":[{\"indexed\":false,\"internalType\":\"uint256\",\"name\":\"epoch\",\"type\":\"uint256\"},{\"indexed\":false,\"internalType\":\"bytes32\",\"name\":\"root\",\"type\":\"bytes32\"}],\"name\":\"RootCommitted\",\"type\":\"event\"},{\"inputs\":[{\"internalType\":\"uint256\",\"name\":\"epoch\",\"type\":\"uint256\"},{\"internalType\":\"bytes32\",\"name\":\"root\",\"type\":\"bytes32\"}],\"name\":\"commit\",\"outputs\":[],\"stateMutability\":\"nonpayable\",\"type\":\"function\"},{\"inputs\":[],\"name\":\"committer\",\"outputs\":[{\"internalType\":\"address\",\"name\":\"\",\"type\":\"address\"}],\"stateMutability\":\"view\",\"type\":\"function\"},{\"inputs\":[{\"internalType\":\"uint256\",\"name\":\"\",\"type\":\"uint256\"}],\"name\":\"epochRoots\",\"outputs\":[{\"internalType\":\"bytes32\",\"name\":\"\",\"type\":\"bytes32\"}],\"stateMutability\":\"view\",\"type\":\"function\"},{\"inputs\":[{\"internalType\":\"uint256\",\"name\":\"epoch\",\"type\":\"uint256\"}],\"name\":\"getRoot\",\"outputs\":[{\"internalType\":\"bytes32\",\"name\":\"\",\"type\":\"bytes32\"}],\"stateMutability\":\"view\",\"type\":\"function\"}]",
	Bin: "0x60a060405234801561001057600080fd5b503373ffffffffffffffffffffffffffffffffffffffff1660808173ffffffffffffffffffffffffffffffffffffffff168152505060805161058361006760003960008181610105015261014501526105836000f3fe608060405234801561001057600080fd5b506004361061004c5760003560e01c806355deccc1146100515780635bc8e8f9146100815780639b24b3b01461009f578063f2f03877146100cf575b600080fd5b61006b600480360381019061006691906102b7565b6100eb565b60405161007891906102fd565b60405180910390f35b610089610103565b6040516100969190610359565b60405180910390f35b6100b960048036038101906100b491906102b7565b610127565b6040516100c691906102fd565b60405180910390f35b6100e960048036038101906100e491906103a0565b610143565b005b60006020528060005260406000206000915090505481565b7f000000000000000000000000000000000000000000000000000000000000000081565b6000806000838152602001908152602001600020549050919050565b7f000000000000000000000000000000000000000000000000000000000000000073ffffffffffffffffffffffffffffffffffffffff163373ffffffffffffffffffffffffffffffffffffffff16146101d1576040517f08c379a00000000000000000000000000000000000000000000000000000000081526004016101c890610463565b60405180910390fd5b6000801b6000808481526020019081526020016000205414610228576040517f08c379a000000000000000000000000000000000000000000000000000000000815260040161021f906104f5565b60405180910390fd5b80600080848152602001908152602001600020819055507f0fc3a893c4ce495b9f2e0ad81215cdc16de15cb2ddbad78d34629b250290d5688282604051610270929190610524565b60405180910390a15050565b600080fd5b6000819050919050565b61029481610281565b811461029f57600080fd5b50565b6000813590506102b18161028b565b92915050565b6000602082840312156102cd576102cc61027c565b5b60006102db848285016102a2565b91505092915050565b6000819050919050565b6102f7816102e4565b82525050565b600060208201905061031260008301846102ee565b92915050565b600073ffffffffffffffffffffffffffffffffffffffff82169050919050565b600061034382610318565b9050919050565b61035381610338565b82525050565b600060208201905061036e600083018461034a565b92915050565b61037d816102e4565b811461038857600080fd5b50565b60008135905061039a81610374565b92915050565b600080604083850312156103b7576103b661027c565b5b60006103c5858286016102a2565b92505060206103d68582860161038b565b9150509250929050565b600082825260208201905092915050565b7f45706f6368416e63686f723a206e6f742074686520617574686f72697a65642060008201527f636f6d6d69747465720000000000000000000000000000000000000000000000602082015250565b600061044d6029836103e0565b9150610458826103f1565b604082019050919050565b6000602082019050818103600083015261047c81610440565b9050919050565b7f45706f6368416e63686f723a2065706f636820616c726561647920636f6d6d6960008201527f7474656400000000000000000000000000000000000000000000000000000000602082015250565b60006104df6024836103e0565b91506104ea82610483565b604082019050919050565b6000602082019050818103600083015261050e816104d2565b9050919050565b61051e81610281565b82525050565b60006040820190506105396000830185610515565b61054660208301846102ee565b939250505056fea26469706673582212203c0117ed89a13231c2ac3a604d27544b2f0a12149ff2c49fea11193b32323fc864736f6c63430008140033",
}

// EpochAnchorABI is the input ABI used to generate the binding from.
// Deprecated: Use EpochAnchorMetaData.ABI instead.
var EpochAnchorABI = EpochAnchorMetaData.ABI

// EpochAnchorBin is the compiled bytecode used for deploying new contracts.
// Deprecated: Use EpochAnchorMetaData.Bin instead.
var EpochAnchorBin = EpochAnchorMetaData.Bin

// DeployEpochAnchor deploys a new Ethereum contract, binding an instance of EpochAnchor to it.
func DeployEpochAnchor(auth *bind.TransactOpts, backend bind.ContractBackend) (common.Address, *types.Transaction, *EpochAnchor, error) {
	parsed, err := EpochAnchorMetaData.GetAbi()
	if err != nil {
		return common.Address{}, nil, nil, err
	}
	if parsed == nil {
		return common.Address{}, nil, nil, errors.New("GetABI returned nil")
	}

	address, tx, contract, err := bind.DeployContract(auth, *parsed, common.FromHex(EpochAnchorBin), backend)
	if err != nil {
		return common.Address{}, nil, nil, err
	}
	return address, tx, &EpochAnchor{EpochAnchorCaller: EpochAnchorCaller{contract: contract}, EpochAnchorTransactor: EpochAnchorTransactor{contract: contract}, EpochAnchorFilterer: EpochAnchorFilterer{contract: contract}}, nil
}

// EpochAnchor is an auto generated Go binding around an Ethereum contract.
type EpochAnchor struct {
	EpochAnchorCaller     // Read-only binding to the contract
	EpochAnchorTransactor // Write-only binding to the contract
	EpochAnchorFilterer   // Log filterer for contract events
}

// EpochAnchorCaller is an auto generated read-only Go binding around an Ethereum contract.
type EpochAnchorCaller struct {
	contract *bind.BoundContract // Generic contract wrapper for the low level calls
}

// EpochAnchorTransactor is an auto generated write-only Go binding around an Ethereum contract.
type EpochAnchorTransactor struct {
	contract *bind.BoundContract // Generic contract wrapper for the low level calls
}

// EpochAnchorFilterer is an auto generated log filtering Go binding around an Ethereum contract events.
type EpochAnchorFilterer struct {
	contract *bind.BoundContract // Generic contract wrapper for the low level calls
}

// EpochAnchorSession is an auto generated Go binding around an Ethereum contract,
// with pre-set call and transact options.
type EpochAnchorSession struct {
	Contract     *EpochAnchor      // Generic contract binding to set the session for
	CallOpts     bind.CallOpts     // Call options to use throughout this session
	TransactOpts bind.TransactOpts // Transaction auth options to use throughout this session
}

// EpochAnchorCallerSession is an auto generated read-only Go binding around an Ethereum contract,
// with pre-set call options.
type EpochAnchorCallerSession struct {
	Contract *EpochAnchorCaller // Generic contract caller binding to set the session for
	CallOpts bind.CallOpts      // Call options to use throughout this session
}

// EpochAnchorTransactorSession is an auto generated write-only Go binding around an Ethereum contract,
// with pre-set transact options.
type EpochAnchorTransactorSession struct {
	Contract     *EpochAnchorTransactor // Generic contract transactor binding to set the session for
	TransactOpts bind.TransactOpts      // Transaction auth options to use throughout this session
}

// EpochAnchorRaw is an auto generated low-level Go binding around an Ethereum contract.
type EpochAnchorRaw struct {
	Contract *EpochAnchor // Generic contract binding to access the raw methods on
}

// EpochAnchorCallerRaw is an auto generated low-level read-only Go binding around an Ethereum contract.
type EpochAnchorCallerRaw struct {
	Contract *EpochAnchorCaller // Generic read-only contract binding to access the raw methods on
}

// EpochAnchorTransactorRaw is an auto generated low-level write-only Go binding around an Ethereum contract.
type EpochAnchorTransactorRaw struct {
	Contract *EpochAnchorTransactor // Generic write-only contract binding to access the raw methods on
}

// NewEpochAnchor creates a new instance of EpochAnchor, bound to a specific deployed contract.
func NewEpochAnchor(address common.Address, backend bind.ContractBackend) (*EpochAnchor, error) {
	contract, err := bindEpochAnchor(address, backend, backend, backend)
	if err != nil {
		return nil, err
	}
	return &EpochAnchor{EpochAnchorCaller: EpochAnchorCaller{contract: contract}, EpochAnchorTransactor: EpochAnchorTransactor{contract: contract}, EpochAnchorFilterer: EpochAnchorFilterer{contract: contract}}, nil
}

// NewEpochAnchorCaller creates a new read-only instance of EpochAnchor, bound to a specific deployed contract.
func NewEpochAnchorCaller(address common.Address, caller bind.ContractCaller) (*EpochAnchorCaller, error) {
	contract, err := bindEpochAnchor(address, caller, nil, nil)
	if err != nil {
		return nil, err
	}
	return &EpochAnchorCaller{contract: contract}, nil
}

// NewEpochAnchorTransactor creates a new write-only instance of EpochAnchor, bound to a specific deployed contract.
func NewEpochAnchorTransactor(address common.Address, transactor bind.ContractTransactor) (*EpochAnchorTransactor, error) {
	contract, err := bindEpochAnchor(address, nil, transactor, nil)
	if err != nil {
		return nil, err
	}
	return &EpochAnchorTransactor{contract: contract}, nil
}

// NewEpochAnchorFilterer creates a new log filterer instance of EpochAnchor, bound to a specific deployed contract.
func NewEpochAnchorFilterer(address common.Address, filterer bind.ContractFilterer) (*EpochAnchorFilterer, error) {
	contract, err := bindEpochAnchor(address, nil, nil, filterer)
	if err != nil {
		return nil, err
	}
	return &EpochAnchorFilterer{contract: contract}, nil
}

// bindEpochAnchor binds a generic wrapper to an already deployed contract.
func bindEpochAnchor(address common.Address, caller bind.ContractCaller, transactor bind.ContractTransactor, filterer bind.ContractFilterer) (*bind.BoundContract, error) {
	parsed, err := EpochAnchorMetaData.GetAbi()
	if err != nil {
		return nil, err
	}
	return bind.NewBoundContract(address, *parsed, caller, transactor, filterer), nil
}

// Call invokes the (constant) contract method with params as input values and
// sets the output to result. The result type might be a single field for simple
// returns, a slice of interfaces for anonymous returns and a struct for named
// returns.
func (_EpochAnchor *EpochAnchorRaw) Call(opts *bind.CallOpts, result *[]interface{}, method string, params ...interface{}) error {
	return _EpochAnchor.Contract.EpochAnchorCaller.contract.Call(opts, result, method, params...)
}

// Transfer initiates a plain transaction to move funds to the contract, calling
// its default method if one is available.
func (_EpochAnchor *EpochAnchorRaw) Transfer(opts *bind.TransactOpts) (*types.Transaction, error) {
	return _EpochAnchor.Contract.EpochAnchorTransactor.contract.Transfer(opts)
}

// Transact invokes the (paid) contract method with params as input values.
func (_EpochAnchor *EpochAnchorRaw) Transact(opts *bind.TransactOpts, method string, params ...interface{}) (*types.Transaction, error) {
	return _EpochAnchor.Contract.EpochAnchorTransactor.contract.Transact(opts, method, params...)
}

// Call invokes the (constant) contract method with params as input values and
// sets the output to result. The result type might be a single field for simple
// returns, a slice of interfaces for anonymous returns and a struct for named
// returns.
func (_EpochAnchor *EpochAnchorCallerRaw) Call(opts *bind.CallOpts, result *[]interface{}, method string, params ...interface{}) error {
	return _EpochAnchor.Contract.contract.Call(opts, result, method, params...)
}

// Transfer initiates a plain transaction to move funds to the contract, calling
// its default method if one is available.
func (_EpochAnchor *EpochAnchorTransactorRaw) Transfer(opts *bind.TransactOpts) (*types.Transaction, error) {
	return _EpochAnchor.Contract.contract.Transfer(opts)
}

// Transact invokes the (paid) contract method with params as input values.
func (_EpochAnchor *EpochAnchorTransactorRaw) Transact(opts *bind.TransactOpts, method string, params ...interface{}) (*types.Transaction, error) {
	return _EpochAnchor.Contract.contract.Transact(opts, method, params...)
}

// Committer is a free data retrieval call binding the contract method 0x5bc8e8f9.
//
// Solidity: function committer() view returns(address)
func (_EpochAnchor *EpochAnchorCaller) Committer(opts *bind.CallOpts) (common.Address, error) {
	var out []interface{}
	err := _EpochAnchor.contract.Call(opts, &out, "committer")

	if err != nil {
		return *new(common.Address), err
	}

	out0 := *abi.ConvertType(out[0], new(common.Address)).(*common.Address)

	return out0, err

}

// Committer is a free data retrieval call binding the contract method 0x5bc8e8f9.
//
// Solidity: function committer() view returns(address)
func (_EpochAnchor *EpochAnchorSession) Committer() (common.Address, error) {
	return _EpochAnchor.Contract.Committer(&_EpochAnchor.CallOpts)
}

// Committer is a free data retrieval call binding the contract method 0x5bc8e8f9.
//
// Solidity: function committer() view returns(address)
func (_EpochAnchor *EpochAnchorCallerSession) Committer() (common.Address, error) {
	return _EpochAnchor.Contract.Committer(&_EpochAnchor.CallOpts)
}

// EpochRoots is a free data retrieval call binding the contract method 0x55deccc1.
//
// Solidity: function epochRoots(uint256 ) view returns(bytes32)
func (_EpochAnchor *EpochAnchorCaller) EpochRoots(opts *bind.CallOpts, arg0 *big.Int) ([32]byte, error) {
	var out []interface{}
	err := _EpochAnchor.contract.Call(opts, &out, "epochRoots", arg0)

	if err != nil {
		return *new([32]byte), err
	}

	out0 := *abi.ConvertType(out[0], new([32]byte)).(*[32]byte)

	return out0, err

}

// EpochRoots is a free data retrieval call binding the contract method 0x55deccc1.
//
// Solidity: function epochRoots(uint256 ) view returns(bytes32)
func (_EpochAnchor *EpochAnchorSession) EpochRoots(arg0 *big.Int) ([32]byte, error) {
	return _EpochAnchor.Contract.EpochRoots(&_EpochAnchor.CallOpts, arg0)
}

// EpochRoots is a free data retrieval call binding the contract method 0x55deccc1.
//
// Solidity: function epochRoots(uint256 ) view returns(bytes32)
func (_EpochAnchor *EpochAnchorCallerSession) EpochRoots(arg0 *big.Int) ([32]byte, error) {
	return _EpochAnchor.Contract.EpochRoots(&_EpochAnchor.CallOpts, arg0)
}

// GetRoot is a free data retrieval call binding the contract method 0x9b24b3b0.
//
// Solidity: function getRoot(uint256 epoch) view returns(bytes32)
func (_EpochAnchor *EpochAnchorCaller) GetRoot(opts *bind.CallOpts, epoch *big.Int) ([32]byte, error) {
	var out []interface{}
	err := _EpochAnchor.contract.Call(opts, &out, "getRoot", epoch)

	if err != nil {
		return *new([32]byte), err
	}

	out0 := *abi.ConvertType(out[0], new([32]byte)).(*[32]byte)

	return out0, err

}

// GetRoot is a free data retrieval call binding the contract method 0x9b24b3b0.
//
// Solidity: function getRoot(uint256 epoch) view returns(bytes32)
func (_EpochAnchor *EpochAnchorSession) GetRoot(epoch *big.Int) ([32]byte, error) {
	return _EpochAnchor.Contract.GetRoot(&_EpochAnchor.CallOpts, epoch)
}

// GetRoot is a free data retrieval call binding the contract method 0x9b24b3b0.
//
// Solidity: function getRoot(uint256 epoch) view returns(bytes32)
func (_EpochAnchor *EpochAnchorCallerSession) GetRoot(epoch *big.Int) ([32]byte, error) {
	return _EpochAnchor.Contract.GetRoot(&_EpochAnchor.CallOpts, epoch)
}

// Commit is a paid mutator transaction binding the contract method 0xf2f03877.
//
// Solidity: function commit(uint256 epoch, bytes32 root) returns()
func (_EpochAnchor *EpochAnchorTransactor) Commit(opts *bind.TransactOpts, epoch *big.Int, root [32]byte) (*types.Transaction, error) {
	return _EpochAnchor.contract.Transact(opts, "commit", epoch, root)
}

// Commit is a paid mutator transaction binding the contract method 0xf2f03877.
//
// Solidity: function commit(uint256 epoch, bytes32 root) returns()
func (_EpochAnchor *EpochAnchorSession) Commit(epoch *big.Int, root [32]byte) (*types.Transaction, error) {
	return _EpochAnchor.Contract.Commit(&_EpochAnchor.TransactOpts, epoch, root)
}

// Commit is a paid mutator transaction binding the contract method 0xf2f03877.
//
// Solidity: function commit(uint256 epoch, bytes32 root) returns()
func (_EpochAnchor *EpochAnchorTransactorSession) Commit(epoch *big.Int, root [32]byte) (*types.Transaction, error) {
	return _EpochAnchor.Contract.Commit(&_EpochAnchor.TransactOpts, epoch, root)
}

// EpochAnchorRootCommittedIterator is returned from FilterRootCommitted and is used to iterate over the raw logs and unpacked data for RootCommitted events raised by the EpochAnchor contract.
type EpochAnchorRootCommittedIterator struct {
	Event *EpochAnchorRootCommitted // Event containing the contract specifics and raw log

	contract *bind.BoundContract // Generic contract to use for unpacking event data
	event    string              // Event name to use for unpacking event data

	logs chan types.Log        // Log channel receiving the found contract events
	sub  ethereum.Subscription // Subscription for errors, completion and termination
	done bool                  // Whether the subscription completed delivering logs
	fail error                 // Occurred error to stop iteration
}

// Next advances the iterator to the subsequent event, returning whether there
// are any more events found. In case of a retrieval or parsing error, false is
// returned and Error() can be queried for the exact failure.
func (it *EpochAnchorRootCommittedIterator) Next() bool {
	// If the iterator failed, stop iterating
	if it.fail != nil {
		return false
	}
	// If the iterator completed, deliver directly whatever's available
	if it.done {
		select {
		case log := <-it.logs:
			it.Event = new(EpochAnchorRootCommitted)
			if err := it.contract.UnpackLog(it.Event, it.event, log); err != nil {
				it.fail = err
				return false
			}
			it.Event.Raw = log
			return true

		default:
			return false
		}
	}
	// Iterator still in progress, wait for either a data or an error event
	select {
	case log := <-it.logs:
		it.Event = new(EpochAnchorRootCommitted)
		if err := it.contract.UnpackLog(it.Event, it.event, log); err != nil {
			it.fail = err
			return false
		}
		it.Event.Raw = log
		return true

	case err := <-it.sub.Err():
		it.done = true
		it.fail = err
		return it.Next()
	}
}

// Error returns any retrieval or parsing error occurred during filtering.
func (it *EpochAnchorRootCommittedIterator) Error() error {
	return it.fail
}

// Close terminates the iteration process, releasing any pending underlying
// resources.
func (it *EpochAnchorRootCommittedIterator) Close() error {
	it.sub.Unsubscribe()
	return nil
}

// EpochAnchorRootCommitted represents a RootCommitted event raised by the EpochAnchor contract.
type EpochAnchorRootCommitted struct {
	Epoch *big.Int
	Root  [32]byte
	Raw   types.Log // Blockchain specific contextual infos
}

// FilterRootCommitted is a free log retrieval operation binding the contract event 0x0fc3a893c4ce495b9f2e0ad81215cdc16de15cb2ddbad78d34629b250290d568.
//
// Solidity: event RootCommitted(uint256 epoch, bytes32 root)
func (_EpochAnchor *EpochAnchorFilterer) FilterRootCommitted(opts *bind.FilterOpts) (*EpochAnchorRootCommittedIterator, error) {

	logs, sub, err := _EpochAnchor.contract.FilterLogs(opts, "RootCommitted")
	if err != nil {
		return nil, err
	}
	return &EpochAnchorRootCommittedIterator{contract: _EpochAnchor.contract, event: "RootCommitted", logs: logs, sub: sub}, nil
}

// WatchRootCommitted is a free log subscription operation binding the contract event 0x0fc3a893c4ce495b9f2e0ad81215cdc16de15cb2ddbad78d34629b250290d568.
//
// Solidity: event RootCommitted(uint256 epoch, bytes32 root)
func (_EpochAnchor *EpochAnchorFilterer) WatchRootCommitted(opts *bind.WatchOpts, sink chan<- *EpochAnchorRootCommitted) (event.Subscription, error) {

	logs, sub, err := _EpochAnchor.contract.WatchLogs(opts, "RootCommitted")
	if err != nil {
		return nil, err
	}
	return event.NewSubscription(func(quit <-chan struct{}) error {
		defer sub.Unsubscribe()
		for {
			select {
			case log := <-logs:
				// New log arrived, parse the event and forward to the user
				event := new(EpochAnchorRootCommitted)
				if err := _EpochAnchor.contract.UnpackLog(event, "RootCommitted", log); err != nil {
					return err
				}
				event.Raw = log

				select {
				case sink <- event:
				case err := <-sub.Err():
					return err
				case <-quit:
					return nil
				}
			case err := <-sub.Err():
				return err
			case <-quit:
				return nil
			}
		}
	}), nil
}

// ParseRootCommitted is a log parse operation binding the contract event 0x0fc3a893c4ce495b9f2e0ad81215cdc16de15cb2ddbad78d34629b250290d568.
//
// Solidity: event RootCommitted(uint256 epoch, bytes32 root)
func (_EpochAnchor *EpochAnchorFilterer) ParseRootCommitted(log types.Log) (*EpochAnchorRootCommitted, error) {
	event := new(EpochAnchorRootCommitted)
	if err := _EpochAnchor.contract.UnpackLog(event, "RootCommitted", log); err != nil {
		return nil, err
	}
	event.Raw = log
	return event, nil
}
