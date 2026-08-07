package payments

import (
    "encoding/json"
    "reflect"
)

func paymentMatchesInput(existing Payment, input CreateInput) bool {
    return existing.Mode == input.Mode &&
        existing.Direction == input.Direction &&
        existing.AmountMinor == input.AmountMinor &&
        existing.Currency == input.Currency &&
        existing.Status == input.Status &&
        stringPtrEqual(existing.Reference, input.Reference) &&
        stringPtrEqual(existing.Provider, input.Provider) &&
        stringPtrEqual(existing.RecordedBy, input.RecordedBy) &&
        rawJSONEqual(existing.ProviderPayload, input.ProviderPayload)
}

func stringPtrEqual(a, b *string) bool {
    if a == nil || b == nil { return a == nil && b == nil }
    return *a == *b
}

func rawJSONEqual(a, b json.RawMessage) bool {
    if len(a) == 0 || string(a) == "null" { a = nil }
    if len(b) == 0 || string(b) == "null" { b = nil }
    if a == nil || b == nil { return a == nil && b == nil }
    var av, bv any
    if json.Unmarshal(a, &av) != nil || json.Unmarshal(b, &bv) != nil { return false }
    return reflect.DeepEqual(av, bv)
}
