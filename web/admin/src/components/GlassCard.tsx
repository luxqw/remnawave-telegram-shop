import type { ComponentChildren, JSX } from "preact";

export function GlassCard(props: {
  children: ComponentChildren;
  style?: JSX.CSSProperties;
  class?: string;
}) {
  return (
    <div class={`glass-card ${props.class ?? ""}`} style={props.style}>
      {props.children}
    </div>
  );
}
