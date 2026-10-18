package proto

// ProtocolVersion is the version of the engine's HTTP protocol. It goes up
// for any change an older app cannot handle: a removed or renamed field,
// route or event, or a changed meaning (engine-design.md, section 0.1).
const ProtocolVersion = 1
