import QtQuick

QtObject {
    // All geometry is logical, global desktop geometry; do not multiply by scale.
    function screenFor(screens, anchor) {
        if (!screens.length) return null;
        if (!anchor) return screens[0];
        let named = screens.find(s => s.name === anchor.screen);
        if (named) return named;
        let containing = screens.find(s => anchor.x >= s.x && anchor.x < s.x+s.width
                                           && anchor.y >= s.y && anchor.y < s.y+s.height);
        if (containing) return containing;
        // Monitor unplugged or stale host coordinates: use the nearest screen.
        function distance(s) {
            let dx = Math.max(s.x-anchor.x, 0, anchor.x-(s.x+s.width));
            let dy = Math.max(s.y-anchor.y, 0, anchor.y-(s.y+s.height));
            return dx*dx+dy*dy;
        }
        return screens.reduce((a,b) => distance(a)<=distance(b)?a:b);
    }
    function geometry(screen, anchor, preferredWidth, preferredHeight, demo) {
        if (!screen) return {x:0,y:0,width:preferredWidth,height:preferredHeight,edge:"top"};
        const gap=8;
        let width=Math.max(1,Math.min(preferredWidth,screen.width-2*gap));
        let height=Math.max(1,Math.min(preferredHeight,screen.height-2*gap));
        let x=screen.width-width-12, y=demo?66:42, edge="top";
        if (anchor) {
            let px=anchor.x-screen.x, py=anchor.y-screen.y;
            let bar=anchor.bar;
            if (bar) {
                edge=bar.w>=bar.h
                    ? (bar.y+bar.h/2<screen.y+screen.height/2?"top":"bottom")
                    : (bar.x+bar.w/2<screen.x+screen.width/2?"left":"right");
            } else {
                let distances=[py,screen.height-py,px,screen.width-px];
                edge=["top","bottom","left","right"][distances.indexOf(Math.min(...distances))];
            }
            // If the screen is small, shrink inward instead of overlapping the bar.
            if (edge==="top") height=Math.max(1,Math.min(height,screen.height-(bar?bar.y+bar.h-screen.y:py+16)-2*gap));
            if (edge==="bottom") height=Math.max(1,Math.min(height,(bar?bar.y-screen.y:py-16)-2*gap));
            if (edge==="left") width=Math.max(1,Math.min(width,screen.width-(bar?bar.x+bar.w-screen.x:px+16)-2*gap));
            if (edge==="right") width=Math.max(1,Math.min(width,(bar?bar.x-screen.x:px-16)-2*gap));
            x=px-width/2; y=py-height/2;
            if (edge==="top") y=(bar?bar.y+bar.h-screen.y:py+16)+gap;
            if (edge==="bottom") y=(bar?bar.y-screen.y:py-16)-height-gap;
            if (edge==="left") x=(bar?bar.x+bar.w-screen.x:px+16)+gap;
            if (edge==="right") x=(bar?bar.x-screen.x:px-16)-width-gap;
        }
        return {x:Math.max(gap,Math.min(x,screen.width-width-gap)),
                y:Math.max(gap,Math.min(y,screen.height-height-gap)),
                width:width,height:height,edge:edge};
    }
}
