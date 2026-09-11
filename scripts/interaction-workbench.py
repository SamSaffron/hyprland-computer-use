#!/usr/bin/env python3
"""Visible disposable GTK fixture. State JSON is evaluator-only, never model input.
Usage: interaction-workbench.py /absolute/path/to/state.json
Requires python-gobject, python-cairo and GTK3. No shell/network/clipboard controls.
"""
import json
import math
import sys
import time
from pathlib import Path

import gi
gi.require_version('Gtk', '3.0')
from gi.repository import Gtk, Gdk

output = Path(sys.argv[1])
checks = dict(modifier_click=False, horizontal_scroll=False, double_click=False,
              triple_click=False, drag_path=False, detail_read=False)
events = []
path = []
pressed = False
code = 'K7M2R9'

def save(event_name, **data):
    events.append(dict(event=event_name, **data))
    output.write_text(json.dumps(dict(checks=checks, events=events), indent=2))

def text(cr, x, y, value, size=21, color=(.88, .92, .98)):
    cr.set_source_rgb(*color)
    cr.select_font_face('DejaVu Sans')
    cr.set_font_size(size)
    cr.move_to(x, y)
    cr.show_text(value)

def draw(widget, cr):
    cr.set_source_rgb(.055, .075, .11); cr.paint()
    text(cr, 35, 45, 'Computer-use interaction workbench', 30)
    text(cr, 35, 83, 'Make each card green. Use the requested gesture, not keyboard substitutes.', 19)
    cards = [('modifier_click',35,120,'1. Ctrl + left click'),
             ('horizontal_scroll',630,120,'2. Scroll RIGHT over this card'),
             ('double_click',35,305,'3. Double left click'),
             ('triple_click',630,305,'4. Triple left click')]
    for key,x,y,label in cards:
        cr.set_source_rgb(*((.05,.36,.23) if checks[key] else (.13,.19,.28)))
        cr.rectangle(x,y,540,140);cr.fill()
        text(cr,x+20,y+55,label)
        text(cr,x+20,y+103,'PASS' if checks[key] else 'Waiting for gesture',18)
    text(cr,35,505,'5. Draw ONE continuous stroke: A → B → C. Keep the button down.',22)
    cr.set_source_rgb(.13,.19,.28);cr.rectangle(35,535,1135,230);cr.fill()
    points=[(110,690),(570,580),(1090,690)]
    cr.set_source_rgb(.32,.42,.55);cr.set_line_width(3);cr.move_to(*points[0])
    for p in points[1:]:cr.line_to(*p)
    cr.stroke()
    for label,(x,y) in zip('ABC',points):
        cr.set_source_rgb(.15,.65,.55);cr.arc(x,y,25,0,2*math.pi);cr.fill();text(cr,x-7,y+7,label,20)
    if path:
        cr.set_source_rgb(.95,.7,.25);cr.set_line_width(4);cr.move_to(*path[0])
        for p in path[1:]:cr.line_to(*p)
        cr.stroke()
    text(cr,50,747,'PASS' if checks['drag_path'] else 'Start at A; visit B; release at C.',17)
    text(cr,1240,155,'6. Read the tiny label',23)
    text(cr,1240,190,'Magnify if needed.',18)
    cr.set_source_rgb(1,1,1);cr.rectangle(1240,220,340,100);cr.fill()
    text(cr,1370,273,code,5,(0,0,0))
    text(cr,1240,365,'Type its six characters below.',18)
    text(cr,1240,470,'PASS' if checks['detail_read'] else 'Waiting for correct code',18)
    text(cr,35,835,f"Result: {sum(checks.values())}/6",26)

def button(widget, event):
    global pressed, path
    x,y=event.x,event.y
    if event.type == Gdk.EventType.BUTTON_PRESS:
        if event.button == 1 and 35<=x<=575 and 120<=y<=260 and event.state&Gdk.ModifierType.CONTROL_MASK:
            checks['modifier_click']=True
        if event.button == 1 and math.hypot(x-110,y-690)<35:
            pressed=True;path=[(x,y)]
    if event.type == Gdk.EventType._2BUTTON_PRESS and 35<=x<=575 and 305<=y<=445:
        checks['double_click']=True
    if event.type == Gdk.EventType._3BUTTON_PRESS and 630<=x<=1170 and 305<=y<=445:
        checks['triple_click']=True
    save('button',kind=str(event.type),x=x,y=y,button=event.button,state=int(event.state))
    widget.queue_draw();return True

def motion(widget,event):
    if pressed:
        path.append((event.x,event.y));widget.queue_draw()
    return True

def release(widget,event):
    global pressed
    if pressed:
        path.append((event.x,event.y))
        checks['drag_path']=any(math.hypot(x-570,y-580)<35 for x,y in path) and math.hypot(event.x-1090,event.y-690)<35
        save('stroke',points=path,passed=checks['drag_path']);pressed=False
    widget.queue_draw();return True

def scroll(widget,event):
    ok,dx,dy=event.get_scroll_deltas()
    if 630<=event.x<=1170 and 120<=event.y<=260 and (event.direction==Gdk.ScrollDirection.RIGHT or (ok and dx>0)):
        checks['horizontal_scroll']=True
    save('scroll',direction=str(event.direction),dx=dx,dy=dy,x=event.x,y=event.y)
    widget.queue_draw();return True

window=Gtk.Window(title='Computer-use interaction workbench')
window.set_default_size(1800,880)
window.connect('destroy',Gtk.main_quit)
fixed=Gtk.Fixed();window.add(fixed)
area=Gtk.DrawingArea();area.set_size_request(1800,880)
area.add_events(Gdk.EventMask.BUTTON_PRESS_MASK|Gdk.EventMask.BUTTON_RELEASE_MASK|Gdk.EventMask.POINTER_MOTION_MASK|Gdk.EventMask.SCROLL_MASK|Gdk.EventMask.SMOOTH_SCROLL_MASK)
area.connect('realize', lambda widget: widget.get_window().set_event_compression(False))
# A drawing canvas must consume every queued motion sample. Default GTK
# compression intentionally discards intermediate motion within a burst.
# Keep this identical in baseline/candidate runs; this does not add timing to
# the backend or claim that arbitrary applications consume unpaced paths.
area.connect('draw',draw);area.connect('button-press-event',button);area.connect('button-release-event',release)
area.connect('motion-notify-event',motion);area.connect('scroll-event',scroll)
fixed.put(area,0,0)
entry=Gtk.Entry();entry.set_size_request(340,45);fixed.put(entry,1240,390)
def changed(e):
    checks['detail_read']=e.get_text()==code
    save('entry',text=e.get_text());area.queue_draw()
entry.connect('changed',changed)
save('start');window.show_all();Gtk.main()
