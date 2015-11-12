# go-pool
fork from golang1.5.1/sync/pool.
goalng 自己的Pool对单个对象，性能很好，但是实际在使用时会出现Gets和Puts的操作，
本Pools是在Pool的基础上，将private变更为[]nterfate{}数组，并且添加Gets和Puts方法。

使用方法：将pools.go, pools_test.go 文件复制到 go/src/sync，然后强制编译sync即可。

Goalng own Pool on a single object, performance is very good, 
but actually occurs in the use of Gets and Puts in the operation,
The Pools are in the Pool, on the basis of the private change to [] nterfate {} array, 
and add the Gets and Puts method。

Usage: use pools. Go, pools_test. Go file is copied to the go/SRC/sync, then forced to compile the sync.
