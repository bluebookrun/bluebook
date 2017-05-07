package http_step

import (
	"fmt"
	log "github.com/Sirupsen/logrus"
	"github.com/bluebookrun/bluebook/bcl"
	"github.com/bluebookrun/bluebook/evaluator/interpolator"
	"github.com/bluebookrun/bluebook/evaluator/proxy"
	"github.com/bluebookrun/bluebook/evaluator/resource"
	"github.com/google/uuid"
	"io/ioutil"
	"net/http"
	"strings"
)

type Resource struct {
	Node       *bcl.BlockNode
	Assertions []*proxy.Proxy
	Outlets    []*proxy.Proxy
	Headers    []string
	Method     string
	Url        string
	Body       string

	attributes map[string]string
}

func New(node *bcl.BlockNode) (resource.Resource, error) {
	d := &Resource{
		Node:       node,
		Assertions: make([]*proxy.Proxy, 0),
		Headers:    make([]string, 0),
		attributes: map[string]string{
			"id": uuid.New().String(),
		},
	}

	for _, expression := range node.Expressions {
		switch {
		case string(expression.Field.Text) == "method":
			d.Method = string(expression.Value.(*bcl.StringNode).Text)
		case string(expression.Field.Text) == "url":
			d.Url = string(expression.Value.(*bcl.StringNode).Text)
		case string(expression.Field.Text) == "assertions":
			listNode := expression.Value.(*bcl.ListNode)
			for _, stepNode := range listNode.Nodes {
				stringNode := stepNode.(*bcl.StringNode)
				d.Assertions = append(d.Assertions, &proxy.Proxy{
					Ref:  string(stringNode.Text),
					Type: proxy.ProxyDriver,
				})
			}
		case string(expression.Field.Text) == "outlets":
			listNode := expression.Value.(*bcl.ListNode)
			for _, stepNode := range listNode.Nodes {
				stringNode := stepNode.(*bcl.StringNode)
				d.Outlets = append(d.Outlets, &proxy.Proxy{
					Ref:  string(stringNode.Text),
					Type: proxy.ProxyDriver,
				})
			}
		case string(expression.Field.Text) == "headers":
			// TODO error if not list
			listNode := expression.Value.(*bcl.ListNode)
			if len(listNode.Nodes)%2 != 0 {
				return nil, fmt.Errorf("headers must contain even number of items")
			}

			for _, node := range listNode.Nodes {
				stringNode := node.(*bcl.StringNode)
				d.Headers = append(d.Headers, string(stringNode.Text))
			}
		case string(expression.Field.Text) == "body":
			d.Body = string(expression.Value.(*bcl.StringNode).Text)
		}
	}

	if d.Method == "" {
		return nil, fmt.Errorf("%s: `method` is required", d.Node.Ref())
	}

	if d.Url == "" {
		return nil, fmt.Errorf("%s: `url` is required", d.Node.Ref())
	}

	return d, nil

}

func (r *Resource) Link(ctx *resource.ExecutionContext) error {
	for i := 0; i < len(r.Assertions); i++ {
		if err := r.Assertions[i].Resolve(ctx); err != nil {
			return err
		}
	}

	for i := 0; i < len(r.Outlets); i++ {
		if err := r.Outlets[i].Resolve(ctx); err != nil {
			return err
		}
	}
	return nil
}

func (r *Resource) GetAttribute(name string) *string {
	value, ok := r.attributes[name]
	if !ok {
		return nil
	}
	return &value
}

func (r *Resource) Exec(ctx *resource.ExecutionContext) error {
	log.WithFields(log.Fields{
		"step": r.Node.Ref(),
	}).Infof("executing")

	bodyReader := strings.NewReader(r.Body)

	url, err := interpolator.Eval(r.Url, ctx)
	if err != nil {
		return err
	}

	method, err := interpolator.Eval(r.Method, ctx)
	if err != nil {
		return err
	}

	// get client via factory from state
	req, err := http.NewRequest(
		method, url, bodyReader)
	if err != nil {
		return err
	}

	for i := 0; i < len(r.Headers); i += 2 {
		name := r.Headers[i]
		value := r.Headers[i+1]
		req.Header.Set(name, value)
	}

	client := &http.Client{}
	resp, err := client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	// todo don't read large bodies
	body, err := ioutil.ReadAll(resp.Body)
	if err != nil {
		return err
	}

	ctx.CurrentResponse = resp
	ctx.CurrentResponseBody = body

	for _, proxy := range r.Assertions {
		err = proxy.Resource.Exec(ctx)
		if err != nil {
			return err
		}
	}

	// capture state for next step
	for _, proxy := range r.Outlets {
		err = proxy.Resource.Exec(ctx)
		if err != nil {
			return err
		}
	}

	return err
}