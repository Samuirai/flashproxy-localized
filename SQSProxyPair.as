package
{
    import flash.events.Event;
    import flash.events.EventDispatcher;
    import flash.events.IOErrorEvent;
    import flash.events.ProgressEvent;
    import flash.events.SecurityErrorEvent;
    import flash.net.Socket;
    import flash.utils.ByteArray;
    
    public class SQSProxyPair extends ProxyPair
    {   
        private var client_socket:Socket;
        
        public function SQSProxyPair(ui:swfcat)
        {
            super(this, ui);
            
            log("Starting SQS proxy pair");
            setup_client_socket();
        }
        
        override public function set client(client_addr:Object):void
        {
            this.client_addr = client_addr;
            log("Client: connecting to " + client_addr.host + ":" + client_addr.port + ".");
            client_socket.connect(client_addr.host, client_addr.port);
        }
        
        override public function close():void
        {
            super.close();
            if (client_socket != null && client_socket.connected) {
                client_socket.close();
            }
            dispatchEvent(new Event(Event.CLOSE));
        }
        
        override public function get connected():Boolean
        {
            return (super.connected && client_socket != null && client_socket.connected);
        }
        
        override protected function transfer_bytes(src:Object, dst:Object, num_bytes:uint):void
        {
            var bytes:ByteArray = new ByteArray();
            
            if (src == null) {
                src = client_socket;
            }
            
            if (dst == null) {
                dst = client_socket;
            }
            
            Socket(src).readBytes(bytes, 0, num_bytes);
            log("SQSProxyPair: transferring " + num_bytes + " bytes.");
            Socket(dst).writeBytes(bytes);
        }
        
        private function setup_client_socket():void
        {
            client_socket = new Socket();

            client_socket.addEventListener(Event.CONNECT, function (e:Event):void {
                log("Client: connected to " + client_addr.host + ":" + client_addr.port + ".");
                if (connected) {
                    dispatchEvent(new Event(Event.CONNECT));
                }
            });
            client_socket.addEventListener(Event.CLOSE, function (e:Event):void {
                log("Client: closed.");
                close();
            });
            client_socket.addEventListener(IOErrorEvent.IO_ERROR, function (e:IOErrorEvent):void {
                log("Client: I/O error: " + e.text + ".");
                close();
            });
            client_socket.addEventListener(SecurityErrorEvent.SECURITY_ERROR, function (e:SecurityErrorEvent):void {
                log("Client: security error: " + e.text + ".");
                close();
            });
            client_socket.addEventListener(ProgressEvent.SOCKET_DATA, client_to_relay);
        }
    }
}