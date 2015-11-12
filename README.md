# go-pool
fork from golang/sync/pool.
goalng 自己的Pool对单个对象，性能很好，但是实际在使用时会出现Gets和Puts的操作，
本Pools是在Pool的基础上，将private变更为[]nterfate{}数组，并且添加Gets和Puts方法，

Goalng own Pool on a single object, performance is very good, 
but actually occurs in the use of Gets and Puts in the operation,
The Pools are in the Pool, on the basis of the private change to [] nterfate {} array, 
and add the Gets and Puts method,
