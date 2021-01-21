package main

import (
	"sync"
	"time"
)

type Sortable interface {
	Less(i, j int) bool
	Len() int
	Swap(i, j int)
}

func Quicksort(slice Sortable) {
	var wg sync.WaitGroup
	wg.Add(1)
	go _quicksort(slice, 0, slice.Len()-1, &wg)
	wg.Wait()

}

func _quicksort(slice Sortable, i, j int, wg *sync.WaitGroup) {
	defer wg.Done()
	if j-i < 2 {
		return
	}
	pivot := i
	start, end := i+1, j
	for start < end {
		if slice.Less(start, pivot) {
			start++
		} else {
			slice.Swap(end, start)
			end--
		}
	}
	if slice.Less(start, pivot) {
		slice.Swap(i, start)
	}
	// fmt.Println(pivot, start, len(slice), slice)
	wg.Add(2)
	time.Sleep(time.Millisecond * 20)
	go _quicksort(slice, i, start, wg)
	go _quicksort(slice, end, j, wg)
}

// func main() {
// 	// var buf [30]int
// 	buf := make([]int, 1005)
// 	// buf := []int{98,85, 540, 694}
// 	for i, _ := range buf {
// 		buf[i] = rand.Intn(100)
// 	}
// 	// buf = []int{81, 94}
// 	fmt.Println(buf)
// 	Quicksort(buf, func(i, j int) bool { return i < j })
// 	fmt.Println(buf)
// }
