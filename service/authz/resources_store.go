package authz

const ResourceStore = "store_product"

var (
	StoreEdit    = Permission{Resource: ResourceStore, Action: "edit"}
	StoreReview  = Permission{Resource: ResourceStore, Action: "review"}
	StorePublish = Permission{Resource: ResourceStore, Action: "publish"}
	StoreRevoke  = Permission{Resource: ResourceStore, Action: "revoke"}
)

func init() {
	RegisterResource(ResourceDefinition{Resource: ResourceStore, LabelKey: "API credit products", Actions: []ActionDefinition{
		{Action: "edit", LabelKey: "Save products"},
		{Action: "review", LabelKey: "Review product revision"},
		{Action: "publish", LabelKey: "Publish product"},
		{Action: "revoke", LabelKey: "Revoke approval"},
	}})
}
