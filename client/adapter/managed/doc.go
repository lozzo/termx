// Package managed 实现 managed WebRTC route attempt adapter。
// Cloud resolution 与 WebRTC setup 构成一个可取消 attempt；外层 route race
// 与 protocol session owner 始终归 client/runtime。
package managed
