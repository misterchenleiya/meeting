import { useEffect, useRef, useState } from "react";
import type { Participant, WhiteboardAction, WhiteboardPoint } from "./types";

type WhiteboardPanelProps = {
  actions: WhiteboardAction[];
  canDraw: boolean;
  participants: Participant[];
  onClear: () => void;
  onSubmitAction: (action: Omit<WhiteboardAction, "id" | "createdBy" | "createdAt">) => void;
};

const boardWidth = 800;
const boardHeight = 420;

export function WhiteboardPanel(props: WhiteboardPanelProps) {
  const canvasRef = useRef<HTMLCanvasElement | null>(null);
  const draftPointsRef = useRef<WhiteboardPoint[]>([]);
  const isDrawingRef = useRef(false);
  const [brushColor, setBrushColor] = useState("#2a5fff");
  const [strokeWidth, setStrokeWidth] = useState(4);

  useEffect(() => {
    redraw(canvasRef.current, props.actions, draftPointsRef.current, brushColor, strokeWidth, props.participants);
  }, [props.actions, brushColor, strokeWidth, props.participants]);

  function handlePointerDown(event: React.PointerEvent<HTMLCanvasElement>) {
    if (!props.canDraw) {
      return;
    }

    isDrawingRef.current = true;
    draftPointsRef.current = [resolvePoint(event)];
    redraw(canvasRef.current, props.actions, draftPointsRef.current, brushColor, strokeWidth, props.participants);
  }

  function handlePointerMove(event: React.PointerEvent<HTMLCanvasElement>) {
    if (!isDrawingRef.current) {
      return;
    }

    draftPointsRef.current = [...draftPointsRef.current, resolvePoint(event)];
    redraw(canvasRef.current, props.actions, draftPointsRef.current, brushColor, strokeWidth, props.participants);
  }

  function handlePointerUp() {
    if (!isDrawingRef.current) {
      return;
    }

    isDrawingRef.current = false;
    if (draftPointsRef.current.length > 1) {
      props.onSubmitAction({
        kind: "stroke",
        color: brushColor,
        strokeWidth,
        points: draftPointsRef.current
      });
    }
    draftPointsRef.current = [];
    redraw(canvasRef.current, props.actions, draftPointsRef.current, brushColor, strokeWidth, props.participants);
  }

  return (
    <article className="panel whiteboard-panel">
      <header className="panel-header">
        <h2>白板</h2>
        <span>{props.actions.length} 条笔迹</span>
      </header>
      <div className="whiteboard-toolbar">
        <label>
          颜色
          <input
            className="color-input"
            onChange={(event) => setBrushColor(event.target.value)}
            type="color"
            value={brushColor}
          />
        </label>
        <label>
          线宽
          <input
            max={12}
            min={2}
            onChange={(event) => setStrokeWidth(Number(event.target.value))}
            type="range"
            value={strokeWidth}
          />
        </label>
        <button className="ghost-button" disabled={!props.canDraw} onClick={props.onClear} type="button">
          清空白板
        </button>
      </div>
      <canvas
        className={`whiteboard-canvas ${props.canDraw ? "is-drawable" : "is-readonly"}`}
        height={boardHeight}
        onPointerCancel={handlePointerUp}
        onPointerDown={handlePointerDown}
        onPointerLeave={handlePointerUp}
        onPointerMove={handlePointerMove}
        onPointerUp={handlePointerUp}
        ref={canvasRef}
        width={boardWidth}
      />
      <p className="empty-copy">
        {props.canDraw
          ? "拖拽鼠标或手指即可在白板上写写画画。"
          : "当前账号没有白板权限，只能查看同步内容。"}
      </p>
    </article>
  );
}

function redraw(
  canvas: HTMLCanvasElement | null,
  actions: WhiteboardAction[],
  draftPoints: WhiteboardPoint[],
  draftColor: string,
  draftWidth: number,
  participants: Participant[]
) {
  if (!canvas) {
    return;
  }

  const context = canvas.getContext("2d");
  if (!context) {
    return;
  }

  context.clearRect(0, 0, canvas.width, canvas.height);
  context.fillStyle = "#fbfcff";
  context.fillRect(0, 0, canvas.width, canvas.height);

  context.strokeStyle = "#d7deed";
  context.lineWidth = 1;
  for (let x = 0; x <= canvas.width; x += 40) {
    context.beginPath();
    context.moveTo(x, 0);
    context.lineTo(x, canvas.height);
    context.stroke();
  }
  for (let y = 0; y <= canvas.height; y += 40) {
    context.beginPath();
    context.moveTo(0, y);
    context.lineTo(canvas.width, y);
    context.stroke();
  }

  for (const action of actions) {
    drawStroke(context, action.points, action.color, action.strokeWidth);
    const label = participants.find((participant) => participant.id === action.createdBy)?.nickname ?? action.createdBy;
    if (action.points[0]) {
      context.fillStyle = "rgba(19, 32, 58, 0.7)";
      context.font = "12px Segoe UI";
      context.fillText(label, action.points[0].x + 8, action.points[0].y - 6);
    }
  }

  if (draftPoints.length > 1) {
    drawStroke(context, draftPoints, draftColor, draftWidth);
  }
}

function drawStroke(
  context: CanvasRenderingContext2D,
  points: WhiteboardPoint[],
  color: string,
  strokeWidth: number
) {
  if (points.length < 2) {
    return;
  }

  context.beginPath();
  context.strokeStyle = color;
  context.lineWidth = strokeWidth;
  context.lineCap = "round";
  context.lineJoin = "round";
  context.moveTo(points[0].x, points[0].y);
  for (const point of points.slice(1)) {
    context.lineTo(point.x, point.y);
  }
  context.stroke();
}

function resolvePoint(event: React.PointerEvent<HTMLCanvasElement>): WhiteboardPoint {
  const rect = event.currentTarget.getBoundingClientRect();
  const scaleX = boardWidth / rect.width;
  const scaleY = boardHeight / rect.height;
  return {
    x: (event.clientX - rect.left) * scaleX,
    y: (event.clientY - rect.top) * scaleY
  };
}
