package edges

import (
	"sort"
	"strings"
)

// cycles finds the cycles of the in-paper uses graph (AX6.3). A proof and the
// items inside it count as the statement that the proof proves, so a proof
// that uses a lemma whose proof uses the theorem is a cycle. Each uses edge in
// a cycle gets the flag cycle.
func (r *resolver) cycles() {
	prefix := r.in.Paper + "#"
	owner := func(id string) string {
		local, ok := strings.CutPrefix(id, prefix)
		if !ok {
			return ""
		}
		it := r.byID[local]
		if it == nil {
			return ""
		}
		if p := r.provesOf(it); p != "" {
			return p
		}
		if it.Proof || it.InProof {
			return ""
		}
		return it.Local
	}
	out := map[string][]string{}
	var nodes []string
	seen := map[string]bool{}
	type pair struct{ a, b string }
	edgeOf := map[pair][]int{}
	for i, e := range r.res.Edges {
		if e.Predicate != Uses {
			continue
		}
		a, b := owner(e.From), owner(e.To)
		if a == "" || b == "" || a == b {
			continue
		}
		for _, n := range []string{a, b} {
			if !seen[n] {
				seen[n] = true
				nodes = append(nodes, n)
			}
		}
		out[a] = append(out[a], b)
		edgeOf[pair{a, b}] = append(edgeOf[pair{a, b}], i)
	}
	for _, scc := range components(nodes, out) {
		if len(scc) < 2 {
			continue
		}
		sort.Slice(scc, func(i, j int) bool { return r.order[r.id(scc[i])] < r.order[r.id(scc[j])] })
		r.res.Cycles = append(r.res.Cycles, scc)
		in := map[string]bool{}
		for _, n := range scc {
			in[n] = true
		}
		for p, is := range edgeOf {
			if in[p.a] && in[p.b] {
				for _, i := range is {
					r.res.Edges[i].Flags = append(r.res.Edges[i].Flags, FlagCycle)
				}
			}
		}
	}
}

// components is the strongly connected components of a graph, by Tarjan.
func components(nodes []string, out map[string][]string) [][]string {
	index := map[string]int{}
	low := map[string]int{}
	on := map[string]bool{}
	var stack []string
	var res [][]string
	n := 0
	var visit func(v string)
	visit = func(v string) {
		index[v], low[v] = n, n
		n++
		stack = append(stack, v)
		on[v] = true
		for _, w := range out[v] {
			if _, ok := index[w]; !ok {
				visit(w)
				low[v] = min(low[v], low[w])
			} else if on[w] {
				low[v] = min(low[v], index[w])
			}
		}
		if low[v] == index[v] {
			var c []string
			for {
				w := stack[len(stack)-1]
				stack = stack[:len(stack)-1]
				on[w] = false
				c = append(c, w)
				if w == v {
					break
				}
			}
			res = append(res, c)
		}
	}
	for _, v := range nodes {
		if _, ok := index[v]; !ok {
			visit(v)
		}
	}
	return res
}
