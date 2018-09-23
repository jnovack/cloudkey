# cloudkey

**cloudkey** is a replacement for `/usr/bin/ck-ui` on your Ubiquity Cloud Key
Generation 2 device.

![screenshot](https://raw.githubusercontent.com/jnovack/cloudkey/master/doc/screenshot.gif)
*Note: Delay is manipulated to show features.*

### Why?

I am an edge case.  I do not use my Cloud Key device for Unifi.  I think it is
a great sexy little hardware device, but to manage a network off of what is
essentially a POE SDCard, you are insane.

Issues with stability are [very well documented](https://help.ubnt.com/hc/en-us/articles/360000128688-UniFi-Troubleshooting-Offline-Cloud-Key-and-Other-Stability-Issues#4).
Using mongodb on an sdcard (limited write cycles) without *automatically*
reparing has lead me to have to recover 4 times in 2 years even with the
secondary USB power from the UPS. That is NOT remotely production stable.
Run Unifi on a server, not a "raspberry pi".

With that said, I am sure you are asking yourself "Why do you have it all?"
The Ubiquity Cloud Key Gen2 is a POE, ARMv7, Single-Board-Computer with
on-board battery backup and a 160x60 framebuffer display built-in.  It is
sexy, for under $200. It looks like an iDevice.

Sure, you can buy a $35 Raspberry Pi, add a case, with a touchscreen, with
a power-supply, and blah blah, but I'll pay for quality and craftmanship so
it does not look like another Frankenstein project.

I can ship it to my grand-parents, tell them to plug one cable into the doo-
hickey and tell them to call their ISP when it has a sad face on it (feature
not developed yet).