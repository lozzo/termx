package history

import vterm "github.com/lozzow/termx/termx-vterm/vterm"

// TerminalSemanticSource is the only terminal semantics source history may
// consume. The domain owner is vterm: it decodes PTY bytes and resize events,
// while history receives already-ordered transactions and must not replay raw.
type TerminalSemanticSource = vterm.TerminalSemanticSource

// TerminalSemanticSize carries the PTY size attached to one semantic
// transaction. History uses it for fixed-grid frame metadata and cursor
// invalidation, not for rewriting committed logical lines.
type TerminalSemanticSize = vterm.TerminalSemanticSize

// TerminalSemanticTransaction is one ordered vterm write/resize boundary. It
// is the truth-source message from parser to classifier/projector.
type TerminalSemanticTransaction = vterm.TerminalSemanticTransaction

// TerminalSemanticOp is an ordered terminal operation emitted by vterm. The
// projector consumes these ops instead of comparing live snapshots.
type TerminalSemanticOp = vterm.TerminalSemanticOp

// TerminalSemanticCell is the vterm cell payload used to build logical-line and
// fixed-grid frame content. It is copied into history-owned payloads as needed.
type TerminalSemanticCell = vterm.TerminalSemanticCell

// TerminalSemanticStyle is the vterm style token shape. History preserves
// terminal semantics and leaves theme default resolution to the viewer.
type TerminalSemanticStyle = vterm.TerminalSemanticStyle

// TerminalSemanticScrollOut proves primary screen ownership moved out of the
// visible area during the same transaction. It is proof, not a second store.
type TerminalSemanticScrollOut = vterm.TerminalSemanticScrollOut

// TerminalSemanticFrame is the current primary or alt fixed-grid frame emitted
// by vterm for a transaction. It can publish current frames but cannot by itself
// create ordinary committed history.
type TerminalSemanticFrame = vterm.TerminalSemanticFrame
